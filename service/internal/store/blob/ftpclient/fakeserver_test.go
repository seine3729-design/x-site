package ftpclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/textproto"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/domain/resource"
)

// testCA 生成只用于测试的自签名证书（127.0.0.1 与 localhost），返回服务端配置与信任它的证书池。
func testCA(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake ftp"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, pool
}

// fakeFTP 是测试用的最小 FTP 服务器：USER/PASS、AUTH TLS、PBSZ/PROT、FEAT、PWD/CWD、EPSV + MLSD。
type fakeFTP struct {
	ln      net.Listener
	mode    resource.TLSMode
	tlsCfg  *tls.Config
	user    string
	pass    string
	dirs    map[string][]string // 目录 → MLSD 行（不含换行）
	noAuth  bool                // 拒绝 AUTH TLS
	strict  bool                // 登录前拒绝 NOOP（530）
	silent  bool                // 接受连接后不发欢迎语
	mu      sync.Mutex
	conns   int
	history []string // 收到的命令（PASS 的参数已替换为 ***）
}

func startFake(t *testing.T, mode resource.TLSMode, tlsCfg *tls.Config, setup func(*fakeFTP)) *fakeFTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFTP{ln: ln, mode: mode, tlsCfg: tlsCfg, user: "xsite", pass: "s3cret", dirs: map[string][]string{
		"/":        {"type=dir;modify=20261020080000; 剧集", "type=file;size=12;modify=20261021090000; readme.txt"},
		"/剧集":      {"type=cdir;modify=20261020080000; /剧集", "type=pdir;modify=20261020080000; ..", "type=file;size=5; b.txt", "type=dir;modify=20261019000000; 霸道总裁", "type=dir; a-剧"},
		"/剧集/霸道总裁": {},
	}}
	if setup != nil {
		setup(f)
	}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeFTP) port() int32 { return int32(f.ln.Addr().(*net.TCPAddr).Port) }

func (f *fakeFTP) connections() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns
}

func (f *fakeFTP) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.history...)
}

func (f *fakeFTP) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns++
		f.mu.Unlock()
		go f.handle(c)
	}
}

func (f *fakeFTP) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	if f.silent {
		time.Sleep(3 * time.Second)
		return
	}
	if f.mode == resource.TLSImplicit {
		c = tls.Server(c, f.tlsCfg)
	}
	tp := textproto.NewConn(c)
	reply := func(format string, args ...any) { _ = tp.PrintfLine(format, args...) }
	reply("220 fake ftp ready")

	var (
		user, cwd = "", "/"
		loggedIn  bool
		protP     bool
		dataLn    net.Listener
	)
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(line, " ")
		cmd = strings.ToUpper(cmd)
		f.mu.Lock()
		if cmd == "PASS" {
			f.history = append(f.history, "PASS ***")
		} else {
			f.history = append(f.history, line)
		}
		f.mu.Unlock()

		switch cmd {
		case "AUTH":
			if f.mode != resource.TLSExplicit || f.noAuth {
				reply("502 AUTH not supported")
				continue
			}
			reply("234 Proceed with negotiation.")
			c = tls.Server(c, f.tlsCfg)
			tp = textproto.NewConn(c)
		case "USER":
			user = arg
			reply("331 Please specify the password.")
		case "PASS":
			if user == f.user && arg == f.pass {
				loggedIn = true
				reply("230 Login successful.")
			} else {
				reply("530 Login incorrect.")
			}
		case "FEAT":
			reply("211-Features:")
			reply(" EPSV")
			reply(" MLST type*;size*;modify*;")
			reply(" UTF8")
			reply("211 End")
		case "TYPE", "OPTS", "PBSZ":
			reply("200 OK.")
		case "PROT":
			protP = arg == "P"
			reply("200 PROT now %s.", arg)
		case "NOOP":
			if f.strict && !loggedIn {
				reply("530 Please login with USER and PASS.")
			} else {
				reply("200 NOOP ok.")
			}
		case "PWD":
			reply(`257 "%s" is the current directory`, cwd)
		case "CWD":
			p := arg
			if !strings.HasPrefix(p, "/") {
				p = path.Join(cwd, p)
			}
			if _, ok := f.dirs[p]; ok && loggedIn {
				cwd = p
				reply("250 Directory successfully changed.")
			} else {
				reply("550 Failed to change directory.")
			}
		case "EPSV":
			if dataLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
				reply("425 Can't open data connection.")
				continue
			}
			reply("229 Entering Extended Passive Mode (|||%d|)", dataLn.Addr().(*net.TCPAddr).Port)
		case "MLSD":
			if dataLn == nil || !loggedIn {
				reply("425 Use PASV or EPSV first.")
				continue
			}
			reply("150 Here comes the directory listing.")
			dc, err := dataLn.Accept()
			_ = dataLn.Close()
			dataLn = nil
			if err != nil {
				return
			}
			if protP {
				dc = tls.Server(dc, f.tlsCfg)
			}
			for _, l := range f.dirs[cwd] {
				_, _ = fmt.Fprintf(dc, "%s\r\n", l)
			}
			_ = dc.Close()
			reply("226 Directory send OK.")
		case "QUIT":
			reply("221 Goodbye.")
			return
		default:
			reply("502 Command not implemented.")
		}
	}
}
