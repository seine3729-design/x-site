package apienc

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// interopFixture 是三端共用的互通样本，Web（apps/src/lib/crypto/testdata）与 APP（mobile/test/fixtures）
// 各有一份内容相同的副本。请求部分由客户端用固定临时私钥生成，服务端必须能逐字节打开；
// 响应部分用本次请求导出的密钥加密，三端都必须能解开。改协议时三份一起更新。
type interopFixture struct {
	Secret            string `json:"secret"`
	Kid               string `json:"kid"`
	PublicKey         string `json:"public_key"`
	EphemeralKeyHex   string `json:"ephemeral_private_key_hex"`
	TS                int64  `json:"ts"`
	Method            string `json:"method"`
	RequestTarget     string `json:"request_target"`
	RequestPlaintext  string `json:"request_plaintext"`
	XEnc              string `json:"x_enc"`
	RequestBodyHex    string `json:"request_body_hex"`
	ResponsePlaintext string `json:"response_plaintext"`
	ResponseBodyHex   string `json:"response_body_hex"`
}

const interopPath = "testdata/interop_v1.json"

func TestInteropFixture(t *testing.T) {
	raw, err := os.ReadFile(interopPath)
	if err != nil {
		t.Fatal(err)
	}
	var f interopFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	kr, err := ParseKeyring(f.Kid + ":" + f.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if got := kr.PublicKeys()[0].PublicKey; got != f.PublicKey {
		t.Fatalf("同一 secret 派生的公钥变了：%s，样本 %s", got, f.PublicKey)
	}

	h, err := ParseHeader(f.XEnc)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := hex.DecodeString(f.RequestBodyHex)
	pt, s, err := kr.Open(h, RequestAAD(f.Method, f.RequestTarget, f.TS), body)
	if err != nil {
		t.Fatalf("打不开客户端生成的请求：%v", err)
	}
	if string(pt) != f.RequestPlaintext {
		t.Fatalf("请求明文不一致：%q", pt)
	}

	resp, _ := hex.DecodeString(f.ResponseBodyHex)
	got, err := s.OpenResponse(resp)
	if err != nil || string(got) != f.ResponsePlaintext {
		t.Fatalf("响应样本解不开：%q %v", got, err)
	}
	// 服务端自己加密的响应，客户端用同样的导出密钥也能解（nonce 随机，只校验能解开）
	sealed, err := s.SealResponse([]byte(f.ResponsePlaintext))
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.OpenResponse(sealed); err != nil || string(again) != f.ResponsePlaintext {
		t.Fatalf("服务端响应往返失败：%v", err)
	}
}
