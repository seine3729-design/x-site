package feedback

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

var (
	handleRe = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)
	slugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
	prefixRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}$`)
	// 纯数字用户名容易与编号混淆，保留词避免冒充官方
	digitsRe     = regexp.MustCompile(`^[0-9]+$`)
	reservedName = map[string]bool{
		"admin": true, "administrator": true, "root": true, "system": true,
		"official": true, "staff": true, "support": true, "feedback": true, "null": true, "undefined": true,
	}
)

// limitOffset 校验页码分页参数并返回规范化后的值。
func limitOffset(page, pageSize int) (p, size, limit, offset int, err error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if page < 1 {
		return 0, 0, 0, 0, badRequest("页码必须大于 0")
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return 0, 0, 0, 0, badRequest("每页条数必须在 1 到 %d 之间", maxPageSize)
	}
	return page, pageSize, pageSize, (page - 1) * pageSize, nil
}

// cleanQuery 去掉控制字符并转义 ILIKE 元字符，返回可直接用于 ILIKE 的模式；为空表示不按文本搜。
func cleanQuery(q string) string {
	q = strings.TrimSpace(stripControl(q))
	if q == "" {
		return ""
	}
	if utf8.RuneCountInString(q) > 60 {
		q = string([]rune(q)[:60])
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// stripControl 去掉不可见控制字符（保留换行与制表符由调用方决定）。
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// cleanText 规范化单行文本：去控制字符、折叠空白、裁剪首尾。
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// cleanBody 规范化多行文本：统一换行、去掉其他控制字符、裁剪首尾空白。
func cleanBody(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.TrimSpace(stripControl(s))
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// validateHandle 校验用户名并返回规范化后的值。
func validateHandle(h string) (string, error) {
	h = strings.TrimSpace(h)
	if !handleRe.MatchString(h) {
		return "", badRequest("用户名只能用 3–32 位字母、数字、下划线或连字符")
	}
	if digitsRe.MatchString(h) {
		return "", badRequest("用户名不能全是数字")
	}
	if reservedName[strings.ToLower(h)] {
		return "", badRequest("该用户名已被保留，请换一个")
	}
	return h, nil
}

// validatePassword 校验密码强度（最少 12 位，允许任意字符，便于使用密码管理器）。
func validatePassword(p string) error {
	if n := runeLen(p); n < 12 || n > 128 {
		return badRequest("密码长度需在 12 到 128 位之间")
	}
	if strings.TrimSpace(p) == "" {
		return badRequest("密码不能只有空格")
	}
	return nil
}

func validateNickname(n string) (string, error) {
	n = cleanText(n)
	if runeLen(n) > 40 {
		return "", badRequest("昵称最多 40 个字")
	}
	return n, nil
}

// validateHTTPSURL 校验「去试用」等外链：只允许 https，且必须有主机名。
func validateHTTPSURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", badRequest("地址必须是完整的 https 链接")
	}
	return u.String(), nil
}

// validateProjectInput 校验并规范化项目保存内容。
func validateProjectInput(in ProjectInput) (ProjectInput, error) {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(in.Slug) {
		return in, badRequest("项目标识只能用 2–48 位小写字母、数字或连字符，且不能以连字符开头")
	}
	in.KeyPrefix = strings.ToUpper(strings.TrimSpace(in.KeyPrefix))
	if !prefixRe.MatchString(in.KeyPrefix) {
		return in, badRequest("反馈编号前缀只能用 2–8 位大写字母或数字，且以字母开头")
	}
	in.Name = cleanText(in.Name)
	if n := runeLen(in.Name); n < 1 || n > 60 {
		return in, badRequest("项目名称需在 1 到 60 个字之间")
	}
	in.Summary = cleanText(in.Summary)
	if runeLen(in.Summary) > 200 {
		return in, badRequest("一句话介绍最多 200 个字")
	}
	in.Description = cleanBody(in.Description)
	if runeLen(in.Description) > 10000 {
		return in, badRequest("项目说明最多 10000 个字")
	}
	in.Cover = strings.TrimSpace(in.Cover)
	if runeLen(in.Cover) > 500 {
		return in, badRequest("封面地址过长")
	}
	tryURL, err := validateHTTPSURL(in.TryURL)
	if err != nil {
		return in, err
	}
	in.TryURL = tryURL
	switch in.Status {
	case ProjectDraft, ProjectRecruiting, ProjectActive, ProjectPaused, ProjectArchived:
	case "":
		in.Status = ProjectDraft
	default:
		return in, badRequest("项目状态不合法")
	}
	switch in.Visibility {
	case VisibilityPublic, VisibilityMembers:
	case "":
		in.Visibility = VisibilityPublic
	default:
		return in, badRequest("项目可见性只能是公开或仅成员")
	}
	switch in.JoinMode {
	case JoinOpen, JoinApplication, JoinInviteOnly, JoinClosed:
	case "":
		in.JoinMode = JoinInviteOnly
	default:
		return in, badRequest("加入方式不合法")
	}
	return in, nil
}

// validatePostInput 校验并规范化动态保存内容。
func validatePostInput(in PostInput) (PostInput, error) {
	if in.ProjectID <= 0 {
		return in, badRequest("请选择所属项目")
	}
	switch in.Kind {
	case PostAnnouncement, PostTestTask, PostChangelog:
	default:
		return in, badRequest("动态类型不合法")
	}
	in.Title = cleanText(in.Title)
	if n := runeLen(in.Title); n < 1 || n > 120 {
		return in, badRequest("标题需在 1 到 120 个字之间")
	}
	in.Body = cleanBody(in.Body)
	if runeLen(in.Body) > 50000 {
		return in, badRequest("正文最多 50000 个字")
	}
	switch in.Status {
	case "draft", "published", "hidden":
	case "":
		in.Status = "draft"
	default:
		return in, badRequest("动态状态不合法")
	}
	switch in.Visibility {
	case VisibilityPublic, VisibilityMembers:
	case "":
		in.Visibility = VisibilityPublic
	default:
		return in, badRequest("动态可见性只能是公开或仅成员")
	}
	return in, nil
}

// validateSubmit 校验并规范化用户提交的反馈。
func validateSubmit(in SubmitInput) (SubmitInput, error) {
	switch in.Kind {
	case KindBug, KindExperience, KindFeature, KindContent, KindAccess, KindCompetitor, KindOther:
	default:
		return in, badRequest("请选择反馈类型")
	}
	in.Title = cleanText(in.Title)
	if n := runeLen(in.Title); n < 4 || n > 160 {
		return in, badRequest("标题需在 4 到 160 个字之间，请用一句话说清问题")
	}
	in.Body = cleanBody(in.Body)
	if n := runeLen(in.Body); n < 1 || n > 20000 {
		return in, badRequest("请填写详细说明，最多 20000 个字")
	}
	if in.Kind == KindBug && runeLen(in.Body) < 10 {
		return in, badRequest("Bug 请补充复现步骤、实际结果与期望结果")
	}
	in.Env = cleanText(in.Env)
	if runeLen(in.Env) > 500 {
		return in, badRequest("设备与网络信息最多 500 个字")
	}
	pageURL, err := cleanPageURL(in.PageURL)
	if err != nil {
		return in, err
	}
	in.PageURL = pageURL
	switch in.Visibility {
	case VisibilityPublic, VisibilityMembers, VisibilityPrivate:
	case "":
		in.Visibility = VisibilityPublic
	default:
		return in, badRequest("可见性不合法")
	}
	return in, nil
}

// validateTriage 校验后台分诊输入。
func validateTriage(in TriageInput, cur Feedback) (TriageInput, error) {
	if in.ExpectedVersion != cur.Version {
		return in, errs.ErrConflict.WithMessage("这条反馈已被其他人更新，请刷新后再保存")
	}
	if in.Status == "" {
		in.Status = cur.Status
	}
	if !CanTransition(cur.Status, in.Status) {
		return in, badRequest("不能从「%s」直接改为「%s」", StatusLabel(cur.Status), StatusLabel(in.Status))
	}
	switch in.Priority {
	case PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow, PriorityUnset:
	case "":
		in.Priority = cur.Priority
	default:
		return in, badRequest("优先级不合法")
	}
	switch in.Visibility {
	case VisibilityPublic, VisibilityMembers, VisibilityPrivate:
	case "":
		in.Visibility = cur.Visibility
	default:
		return in, badRequest("可见性不合法")
	}
	if in.Status == StatusDuplicate {
		if in.DuplicateOf <= 0 {
			return in, badRequest("标记重复时必须选择主反馈")
		}
		if in.DuplicateOf == cur.ID {
			return in, badRequest("不能把反馈标记为自己的重复项")
		}
	} else {
		in.DuplicateOf = 0
	}
	if in.Status == StatusDeclined && cleanText(in.Note) == "" {
		return in, badRequest("暂不采纳时必须填写可公开的原因")
	}
	in.Note = cleanBody(in.Note)
	if runeLen(in.Note) > 1000 {
		return in, badRequest("公开说明最多 1000 个字")
	}
	in.InternalNote = cleanBody(in.InternalNote)
	if runeLen(in.InternalNote) > 1000 {
		return in, badRequest("内部备注最多 1000 个字")
	}
	return in, nil
}

// sortKey 把前台排序参数映射为白名单值，避免把用户输入拼进 SQL。
func sortKey(s string, allowOldest bool) (string, error) {
	switch s {
	case "", "latest":
		return "latest", nil
	case "newest":
		return "newest", nil
	case "votes":
		return "votes", nil
	case "oldest":
		if allowOldest {
			return "oldest", nil
		}
	}
	return "", badRequest("排序方式不合法")
}

// cleanPageURL 规范化「出问题的页面地址」。
// 查询串与片段一律丢弃：它们经常带令牌、邮箱、订单号（feedback/docs/09 第 10.1 节数据最小化）。
func cleanPageURL(raw string) (string, error) {
	raw = strings.TrimSpace(stripControl(raw))
	if raw == "" {
		return "", nil
	}
	if runeLen(raw) > 500 {
		return "", badRequest("页面地址过长")
	}
	// 站内相对路径原样保留（只留路径部分）
	if strings.HasPrefix(raw, "/") {
		if i := strings.IndexAny(raw, "?#"); i >= 0 {
			raw = raw[:i]
		}
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", badRequest("页面地址需要是 http(s) 链接或站内路径")
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String(), nil
}

// validateTopicInput 校验并规范化议题保存内容。
func validateTopicInput(in TopicInput) (TopicInput, error) {
	if in.ProjectID <= 0 {
		return in, badRequest("请选择所属项目")
	}
	in.Title = cleanText(in.Title)
	if n := runeLen(in.Title); n < 1 || n > 160 {
		return in, badRequest("议题标题需在 1 到 160 个字之间")
	}
	in.Body = cleanBody(in.Body)
	if runeLen(in.Body) > 20000 {
		return in, badRequest("议题说明最多 20000 个字")
	}
	in.AnswerHint = cleanText(in.AnswerHint)
	if runeLen(in.AnswerHint) > 200 {
		return in, badRequest("回答引导语最多 200 个字")
	}
	switch in.Status {
	case TopicDraft, TopicOpen, TopicClosed, TopicArchived:
	case "":
		in.Status = TopicDraft
	default:
		return in, badRequest("议题状态不合法")
	}
	switch in.Visibility {
	case VisibilityPublic, VisibilityMembers:
	case "":
		in.Visibility = VisibilityPublic
	default:
		return in, badRequest("议题可见性只能是公开或仅成员")
	}
	return in, nil
}

// validateAnswerBody 校验一条回答的正文。
func validateAnswerBody(body string) (string, error) {
	body = cleanBody(body)
	if n := runeLen(body); n < 2 || n > 5000 {
		return "", badRequest("回答需在 2 到 5000 个字之间")
	}
	return body, nil
}

// validateAPIKeyInput 校验并规范化 API Key 的保存内容。
func validateAPIKeyInput(in APIKeyInput) (APIKeyInput, error) {
	if in.ProjectID <= 0 {
		return in, badRequest("请选择这个 Key 收集到哪个项目")
	}
	in.Name = cleanText(in.Name)
	if n := runeLen(in.Name); n < 1 || n > 60 {
		return in, badRequest("名称需在 1 到 60 个字之间")
	}
	switch in.DefaultVisibility {
	case VisibilityPublic, VisibilityMembers, VisibilityPrivate:
	case "":
		in.DefaultVisibility = VisibilityMembers
	default:
		return in, badRequest("默认可见性不合法")
	}
	switch in.Status {
	case "active", "inactive":
	case "":
		in.Status = "active"
	default:
		return in, badRequest("状态不合法")
	}
	if in.DailyLimit == 0 {
		in.DailyLimit = 500
	}
	if in.DailyLimit < 1 || in.DailyLimit > 100000 {
		return in, badRequest("每日上限需在 1 到 100000 之间")
	}
	origins := make([]string, 0, len(in.AllowedOrigins))
	for _, o := range in.AllowedOrigins {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return in, badRequest("来源 %q 需要写成 https://example.com 这样的形式", o)
		}
		// 只保留 scheme://host[:port]，Origin 不含路径
		origins = append(origins, u.Scheme+"://"+u.Host)
	}
	if len(origins) > 20 {
		return in, badRequest("来源白名单最多 20 条")
	}
	in.AllowedOrigins = origins
	return in, nil
}

// validateIntake 校验外部网站提交的内容。
func validateIntake(in IntakeInput) (IntakeInput, error) {
	switch in.Kind {
	case KindBug, KindExperience, KindFeature, KindContent, KindAccess, KindCompetitor, KindOther:
	case "":
		in.Kind = KindOther
	default:
		return in, badRequest("反馈类型不合法")
	}
	in.Title = cleanText(in.Title)
	if n := runeLen(in.Title); n < 4 || n > 160 {
		return in, badRequest("标题需在 4 到 160 个字之间")
	}
	in.Body = cleanBody(in.Body)
	if n := runeLen(in.Body); n < 1 || n > 20000 {
		return in, badRequest("详细内容不能为空，最多 20000 个字")
	}
	in.Env = cleanText(in.Env)
	if runeLen(in.Env) > 500 {
		return in, badRequest("设备与网络信息最多 500 个字")
	}
	pageURL, err := cleanPageURL(in.PageURL)
	if err != nil {
		return in, err
	}
	in.PageURL = pageURL
	in.Contact = cleanText(in.Contact)
	if runeLen(in.Contact) > 120 {
		return in, badRequest("联系方式最多 120 个字")
	}
	return in, nil
}
