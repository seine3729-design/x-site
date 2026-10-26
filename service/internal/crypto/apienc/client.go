package apienc

import (
	"crypto/hpke"
	"encoding/base64"
	"fmt"
)

// ClientRequest 是客户端侧封装好的一次请求。服务端本身不需要发起加密请求，
// 这里供测试、联调工具与服务间排障使用，行为与 Web / APP 客户端一致。
type ClientRequest struct {
	Header  string // X-Enc 头的值
	Body    []byte // 加密后的请求体；明文为空时为 nil
	Session *Session
}

// NewClientRequest 用服务端公钥（base64url）封装一次请求。
func NewClientRequest(kid, publicKey string, ts int64, method, requestURI string, plaintext []byte) (*ClientRequest, error) {
	raw, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, fmt.Errorf("apienc: 公钥格式错误: %w", err)
	}
	pk, err := kem.NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("apienc: 公钥无效: %w", err)
	}
	enc, s, err := hpke.NewSender(pk, kdf, aead, []byte(Info))
	if err != nil {
		return nil, err
	}
	aad := RequestAAD(method, requestURI, ts)
	tag, err := s.Seal(aad, nil)
	if err != nil {
		return nil, err
	}
	req := &ClientRequest{Header: FormatHeader(kid, ts, enc, tag)}
	if len(plaintext) > 0 {
		req.Body, err = s.Seal(aad, plaintext)
		if err != nil {
			return nil, err
		}
	}
	key, err := s.Export(ResponseLabel, keySize)
	if err != nil {
		return nil, err
	}
	req.Session = &Session{respKey: key}
	return req, nil
}
