package transcode

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// encryptSegment 按 HLS METHOD=AES-128 加密整个切片：AES-128-CBC，PKCS#7 填充（RFC 8216 第 4.3.2.4 节）。
func encryptSegment(key []byte, iv [16]byte, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("内容密钥无效: %w", err)
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("内容密钥应为 16 字节，得到 %d", len(key))
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize // 1–16，整块时补一整块
	out := make([]byte, len(plain)+pad)
	copy(out, plain)
	for i := len(plain); i < len(out); i++ {
		out[i] = byte(pad)
	}
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(out, out)
	return out, nil
}
