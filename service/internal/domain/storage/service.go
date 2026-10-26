package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"xsite/service/internal/crypto/envelope"
	"xsite/service/internal/netguard"
)

// AAD 绑定表名与列名，令读写凭据与只读凭据的密文不能互换、也不能挪到其他表（docs/10 第 3.2 节）。
var (
	writeAAD = []byte("storage_provider|credential_enc")
	readAAD  = []byte("storage_provider|read_credential_enc")
)

// Policy 是随运行环境变化的规则。
type Policy struct {
	// HTTPSOnly 为 true 时自定义端点只允许 https（非开发环境）。
	HTTPSOnly bool
	// Egress 是出站地址策略：端点是被禁止的 IP 字面量时保存即拒绝；域名由连通性测试在连接前解析校验。
	Egress netguard.Policy
}

// Service 是存储源的后台规则。keyring 用于凭据信封加密，prober 执行连通性测试。
type Service struct {
	repo    Repo
	keyring *envelope.Keyring
	prober  Prober
	policy  Policy
}

func NewService(repo Repo, keyring *envelope.Keyring, prober Prober, policy Policy) *Service {
	return &Service{repo: repo, keyring: keyring, prober: prober, policy: policy}
}

// List 返回存储源列表的一页（按编号倒序）。
func (s *Service) List(ctx context.Context, f Filter) (Page[Provider], error) {
	pattern, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Provider]{}, err
	}
	if f.Type != "" && f.Type != TypeS3 && f.Type != TypeR2 && f.Type != TypeCustom {
		return Page[Provider]{}, badRequest("类型筛选无效")
	}
	if f.Status != "" && f.Status != StatusActive && f.Status != StatusDraining && f.Status != StatusDisabled {
		return Page[Provider]{}, badRequest("状态筛选无效")
	}
	page, size, limit, offset, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Provider]{}, err
	}
	rows, total, err := s.repo.List(ctx, Query{Pattern: pattern, Type: f.Type, Status: f.Status, Limit: limit, Offset: offset})
	if err != nil {
		return Page[Provider]{}, err
	}
	list := make([]Provider, len(rows))
	for i, r := range rows {
		list[i] = r.Provider()
	}
	return Page[Provider]{List: list, Total: total, Page: page, PageSize: size}, nil
}

// Get 返回单个存储源（不含凭据）。
func (s *Service) Get(ctx context.Context, id int64) (Provider, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return Provider{}, err
	}
	return row.Provider(), nil
}

// Create 新建存储源。除 HTTP 源外都必须提供读写凭据。
func (s *Service) Create(ctx context.Context, in Input) (Provider, error) {
	in, err := s.clean(in)
	if err != nil {
		return Provider{}, err
	}
	if !isHTTPOrigin(in.Type, in.Subtype) && in.WriteCred == nil {
		return Provider{}, badRequest("请填写读写凭据（Access Key 与 Secret Key）")
	}
	row := rowFromInput(in)
	if in.WriteCred != nil {
		row.CredentialEnc = s.seal(writeAAD, *in.WriteCred)
		row.CredTail = secretTail(in.WriteCred.SecretAccessKey)
	}
	if in.ReadCred != nil {
		row.ReadCredentialEnc = s.seal(readAAD, *in.ReadCred)
		row.ReadCredTail = secretTail(in.ReadCred.SecretAccessKey)
	}
	out, err := s.repo.Create(ctx, row)
	if err != nil {
		return Provider{}, err
	}
	return out.Provider(), nil
}

// Update 整体保存存储源，返回修改前后的记录。凭据留空（WriteCred / ReadCred 为 nil）时保持原密文不变：
// 表单没带上凭据不等于要清空，和资源机凭据标识的处理一致。
func (s *Service) Update(ctx context.Context, id int64, in Input) (before, after Provider, err error) {
	if in, err = s.clean(in); err != nil {
		return
	}
	beforeRow, err := s.repo.Get(ctx, id)
	if err != nil {
		return Provider{}, Provider{}, err
	}
	row := rowFromInput(in)
	if in.WriteCred != nil {
		row.CredentialEnc = s.seal(writeAAD, *in.WriteCred)
		row.CredTail = secretTail(in.WriteCred.SecretAccessKey)
	} else {
		row.CredentialEnc = beforeRow.CredentialEnc
		row.CredTail = beforeRow.CredTail
	}
	if in.ReadCred != nil {
		row.ReadCredentialEnc = s.seal(readAAD, *in.ReadCred)
		row.ReadCredTail = secretTail(in.ReadCred.SecretAccessKey)
	} else {
		row.ReadCredentialEnc = beforeRow.ReadCredentialEnc
		row.ReadCredTail = beforeRow.ReadCredTail
	}
	afterRow, err := s.repo.Update(ctx, id, row)
	if err != nil {
		return Provider{}, Provider{}, err
	}
	return beforeRow.Provider(), afterRow.Provider(), nil
}

// Delete 删除存储源（不进回收站），返回删除前的记录。
// 还有媒体副本时拒绝删除（docs/12 第 5 节第 5 条：需先设为 draining 并迁移），避免留下指向不存在存储源的副本记录。
func (s *Service) Delete(ctx context.Context, id int64) (Provider, error) {
	if n, err := s.repo.CountReplicasByProvider(ctx, id); err == nil && n > 0 {
		return Provider{}, badRequest("该存储源上还有 %d 个媒体副本，请先设为停写（draining）并迁移到其他存储源", n)
	}
	row, err := s.repo.Delete(ctx, id)
	if err != nil {
		return Provider{}, err
	}
	return row.Provider(), nil
}

// Test 连通性测试：解密凭据后在 API 进程内执行写入 / 读取 / 预签名 / 删除（docs/12 第 4.4 节）。
func (s *Service) Test(ctx context.Context, id int64) (ProbeResult, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	req := ProbeRequest{
		Type: row.Type, Subtype: row.Subtype, Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket,
		AccountID: row.AccountID, PathStyle: row.PathStyle, PathPrefix: row.PathPrefix, TTL: row.MaxPresignTTL,
	}
	if len(row.CredentialEnc) > 0 {
		if req.Write, err = s.open(writeAAD, row.CredentialEnc); err != nil {
			return ProbeResult{}, err
		}
	} else if !isHTTPOrigin(row.Type, row.Subtype) {
		return ProbeResult{}, badRequest("尚未配置读写凭据，无法测试连通性")
	}
	if len(row.ReadCredentialEnc) > 0 {
		if req.Read, err = s.open(readAAD, row.ReadCredentialEnc); err != nil {
			return ProbeResult{}, err
		}
	} else {
		req.Read = req.Write
	}
	return s.prober.Probe(ctx, req), nil
}

// ReadCredential 返回存储源的只读凭据（实现 CredentialOpener，供签名服务生成预签名）。
// 没有单独配置只读凭据时回退到读写凭据；两者都没有时报错。
func (s *Service) ReadCredential(row Row) (Credential, error) {
	if len(row.ReadCredentialEnc) > 0 {
		return s.open(readAAD, row.ReadCredentialEnc)
	}
	if len(row.CredentialEnc) > 0 {
		return s.open(writeAAD, row.CredentialEnc)
	}
	return Credential{}, badRequest("存储源「%s」尚未配置凭据，无法生成预签名地址", row.Name)
}

// WriteCredential 返回存储源的读写凭据（上传与复制用，S1-6f）。没有配置时报错。
func (s *Service) WriteCredential(row Row) (Credential, error) {
	if len(row.CredentialEnc) == 0 {
		return Credential{}, badRequest("存储源「%s」尚未配置读写凭据，无法写入", row.Name)
	}
	return s.open(writeAAD, row.CredentialEnc)
}

// seal 加密一组凭据（access_key_id + secret_access_key 一起）。
func (s *Service) seal(aad []byte, c Credential) []byte {
	b, _ := json.Marshal(credJSON{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey})
	return s.keyring.Seal(aad, b)
}

// open 解密一组凭据；密文损坏或密钥缺失时返回带上下文的错误（映射为 500，细节进日志，不回显）。
func (s *Service) open(aad, enc []byte) (Credential, error) {
	b, err := s.keyring.Open(aad, enc)
	if err != nil {
		return Credential{}, fmt.Errorf("解密存储源凭据失败（STORAGE_KEKS 是否变更？）: %w", err)
	}
	var j credJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return Credential{}, fmt.Errorf("解析存储源凭据失败: %w", err)
	}
	return Credential{AccessKeyID: j.AccessKeyID, SecretAccessKey: j.SecretAccessKey}, nil
}

type credJSON struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func rowFromInput(in Input) Row {
	return Row{
		Name: in.Name, Type: in.Type, Subtype: in.Subtype, Purpose: in.Purpose, Role: in.Role,
		Endpoint: in.Endpoint, Region: in.Region, Bucket: in.Bucket, AccountID: in.AccountID,
		PathStyle: in.PathStyle, PathPrefix: in.PathPrefix, MaxPresignTTL: in.MaxPresignTTL, Status: in.Status,
	}
}

func isHTTPOrigin(t Type, sub Subtype) bool { return t == TypeCustom && sub == SubtypeHTTPOrigin }
