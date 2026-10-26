// Package pow 是自建的工作量证明（Proof of Work）人机验证（docs/03 第 2.2 节，S2-2）。
//
// 服务端下发一个随机挑战串与难度（前导零 bit 数）；客户端找一个 solution 使 SHA-256(challenge ":" solution)
// 的二进制前缀有 difficulty 个 0。校验只需一次哈希，求解需约 2^difficulty 次，用算力换「这是真人/真客户端」的弱证明，
// 挡住自动化撞库。难度与一次性由上层（challenge.Guard）控制。
package pow

import (
	"crypto/sha256"
	"math/bits"
)

// MaxDifficulty 是可接受的难度上限，防止上层传入离谱值导致校验恒假 / 求解不可能。
const MaxDifficulty = 32

// Verify 校验 solution 是否满足 challenge 的难度：SHA-256(challenge + ":" + solution) 的前 difficulty 个 bit 为 0。
func Verify(challenge, solution string, difficulty int) bool {
	if difficulty <= 0 || difficulty > MaxDifficulty || solution == "" {
		return false
	}
	sum := sha256.Sum256([]byte(challenge + ":" + solution))
	return leadingZeros(sum[:]) >= difficulty
}

// leadingZeros 返回字节切片二进制表示的前导零个数。
func leadingZeros(b []byte) int {
	n := 0
	for _, x := range b {
		lz := bits.LeadingZeros8(x)
		n += lz
		if lz != 8 {
			break
		}
	}
	return n
}
