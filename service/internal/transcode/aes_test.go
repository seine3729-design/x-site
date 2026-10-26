package transcode

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decryptSegment 是播放器的解密过程：AES-128-CBC 后去掉 PKCS#7 填充（测试用）。
func decryptSegment(t *testing.T, key []byte, iv [16]byte, enc []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil || len(enc) == 0 || len(enc)%aes.BlockSize != 0 {
		t.Fatalf("密文长度 %d 不是块大小的整数倍：%v", len(enc), err)
	}
	out := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(out, enc)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		t.Fatalf("PKCS#7 填充无效：%d", pad)
	}
	return out[:len(out)-pad]
}

func TestEncryptSegment_NISTVector(t *testing.T) {
	// NIST SP 800-38A F.2.1 CBC-AES128.Encrypt：前两块密文与标准向量一致，之后是一整块填充
	key := mustHex(t, "2b7e151628aed2a6abf7158809cf4f3c")
	var iv [16]byte
	copy(iv[:], mustHex(t, "000102030405060708090a0b0c0d0e0f"))
	plain := mustHex(t, "6bc1bee22e409f96e93d7e117393172aae2d8a571e03ac9c9eb76fac45af8e51")
	enc, err := encryptSegment(key, iv, plain)
	if err != nil {
		t.Fatal(err)
	}
	if want := "7649abac8119b246cee98e9b12e9197d5086cb9b507219ee95db113a917678b2"; hex.EncodeToString(enc[:32]) != want {
		t.Fatalf("与 NIST 向量不一致：%x", enc[:32])
	}
	if len(enc) != 48 {
		t.Fatalf("整块明文要补一整块填充，密文应为 48 字节，得到 %d", len(enc))
	}
	if got := decryptSegment(t, key, iv, enc); !bytes.Equal(got, plain) {
		t.Fatal("解密后应还原明文")
	}
}

func TestEncryptSegment_Padding(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	var iv [16]byte
	for _, n := range []int{0, 1, 15, 17, 1000} {
		plain := bytes.Repeat([]byte{0x5a}, n)
		enc, err := encryptSegment(key, iv, plain)
		if err != nil {
			t.Fatal(err)
		}
		if want := (n/16 + 1) * 16; len(enc) != want {
			t.Fatalf("明文 %d 字节，密文应为 %d，得到 %d", n, want, len(enc))
		}
		if got := decryptSegment(t, key, iv, enc); !bytes.Equal(got, plain) {
			t.Fatalf("明文 %d 字节：解密不一致", n)
		}
	}
	if _, err := encryptSegment(bytes.Repeat([]byte{1}, 32), iv, []byte("x")); err == nil {
		t.Fatal("HLS AES-128 只接受 16 字节密钥")
	}
}
