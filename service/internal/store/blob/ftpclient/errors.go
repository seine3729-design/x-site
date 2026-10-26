package ftpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/netguard"
)

// 服务器原始应答最多保留的字数。
const maxReplyRunes = 200

var (
	errNoCredentials = errors.New("没有配置账号密码")
	errTLSPolicy     = errors.New("当前环境只允许显式 TLS（FTPS）连接资源机，请把加密方式改为显式 TLS")
	errTLSMode       = errors.New("加密方式无效")
	errActiveMode    = errors.New("目前只支持被动模式")
	errPort          = errors.New("端口无效")
)

// dirError 表示进入目录失败（CWD 被拒绝）。
type dirError struct{ err error }

func (e *dirError) Error() string { return "进入目录失败: " + e.err.Error() }
func (e *dirError) Unwrap() error { return e.err }

// describe 把错误转成给运营看的中文说明：不含内部细节，服务器应答截断后保留，便于技术排查。
func (r *run) describe(step resource.StepName, err error) string {
	var (
		blocked   *netguard.BlockedError
		dnsErr    *net.DNSError
		reply     *textproto.Error
		dir       *dirError
		unknownCA x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		invalid   x509.CertificateInvalidError
		verify    *tls.CertificateVerificationError
		record    tls.RecordHeaderError
		netErr    net.Error
	)
	switch {
	case errors.Is(err, errNoCredentials):
		user, password := resource.CredEnv(r.req.Server.CredRef)
		return fmt.Sprintf("worker 中没有配置这台资源机的账号密码：请把 %s 和 %s 发给技术，在 worker 的密钥管理中配置后重试", user, password)
	case errors.Is(err, errTLSPolicy), errors.Is(err, errTLSMode):
		return err.Error()
	case errors.Is(err, errActiveMode):
		return "目前只支持被动模式：主动模式需要资源机回连 worker"
	case errors.Is(err, errPort):
		return "端口无效（1–65535）"
	case errors.As(err, &blocked):
		return fmt.Sprintf("地址 %s 属于内网或保留地址段，当前环境禁止连接", blocked.Addr)
	case errors.As(err, &dnsErr):
		switch {
		case dnsErr.IsNotFound:
			return "域名不存在，请检查地址"
		case dnsErr.IsTimeout:
			return "域名解析超时"
		}
		return "域名解析失败"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return "超时：资源机没有在规定时间内响应"
	case errors.Is(err, context.Canceled):
		return "操作已取消"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "连接被拒绝：端口没有开放，或被防火墙拦截"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "网络不可达"
	case errors.As(err, &unknownCA):
		return "证书不是受信任的机构签发的（自签名证书需要换成正式证书）"
	case errors.As(err, &hostErr):
		return "证书与地址不符：请填写证书上的域名"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "证书已过期或尚未生效"
		}
		return "证书无效"
	case errors.As(err, &verify):
		return "证书校验失败"
	case errors.As(err, &record):
		return "对方没有按 TLS 应答：请检查加密方式与端口（隐式 TLS 常用 990，显式 TLS 常用 21）"
	case errors.As(err, &dir):
		if errors.As(err, &reply) {
			return "无法进入目录：" + replyText(reply)
		}
		return "无法进入目录"
	case errors.As(err, &reply):
		switch {
		case step == resource.StepConnect && r.req.Server.TLS == resource.TLSExplicit && reply.Code >= 500:
			return "服务器不支持显式 TLS（AUTH TLS 被拒绝）：" + replyText(reply)
		case step == resource.StepLogin:
			return "登录失败：" + replyText(reply)
		case step == resource.StepList:
			return "读取目录失败：" + replyText(reply)
		}
		return "服务器拒绝：" + replyText(reply)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET), errors.Is(err, net.ErrClosed):
		return "连接被资源机关闭"
	case strings.Contains(err.Error(), "tls: "):
		return "TLS 握手失败"
	case step == resource.StepLogin:
		// jlaffaye/ftp 对 USER 的非预期应答只返回服务器原文
		return "登录失败：" + sanitize(err.Error())
	}
	return "连接失败，请稍后重试"
}

func replyText(e *textproto.Error) string {
	return fmt.Sprintf("%d %s", e.Code, sanitize(e.Msg))
}

// sanitize 去掉控制字符、合并空白，并截断到 maxReplyRunes 个字。
func sanitize(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if utf8.RuneCountInString(s) > maxReplyRunes {
		s = string([]rune(s)[:maxReplyRunes]) + "…"
	}
	return s
}
