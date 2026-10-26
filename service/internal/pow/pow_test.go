package pow

import (
	"strconv"
	"testing"
)

// solve 暴力求解（测试与客户端参考用）：找到满足难度的最小数字解。
func solve(challenge string, difficulty int) string {
	for i := 0; ; i++ {
		s := strconv.Itoa(i)
		if Verify(challenge, s, difficulty) {
			return s
		}
	}
}

func TestVerify_SolvedPasses(t *testing.T) {
	for _, d := range []int{1, 8, 12, 16} {
		sol := solve("nonce-abc", d)
		if !Verify("nonce-abc", sol, d) {
			t.Fatalf("difficulty=%d 求得的解应通过", d)
		}
		// 解绑定 challenge：同一个解换一个 challenge 在较高难度下极大概率失效
		if d >= 12 && Verify("nonce-xyz", sol, d) {
			t.Fatalf("difficulty=%d 的解不应对另一 challenge 通过", d)
		}
	}
}

func TestVerify_Rejects(t *testing.T) {
	if Verify("n", "", 8) {
		t.Fatal("空解应拒绝")
	}
	if Verify("n", "x", 0) {
		t.Fatal("难度 0 应拒绝")
	}
	if Verify("n", "x", MaxDifficulty+1) {
		t.Fatal("超上限难度应拒绝")
	}
	if Verify("nonce-abc", "definitely-wrong-solution", 16) {
		t.Fatal("随意解在难度 16 下应极大概率失败")
	}
}

func TestLeadingZeros(t *testing.T) {
	for _, tt := range []struct {
		b []byte
		n int
	}{
		{[]byte{0xff}, 0},
		{[]byte{0x7f}, 1},
		{[]byte{0x00, 0xff}, 8},
		{[]byte{0x00, 0x0f}, 12},
		{[]byte{0x00, 0x00}, 16},
	} {
		if got := leadingZeros(tt.b); got != tt.n {
			t.Errorf("leadingZeros(%x) = %d，期望 %d", tt.b, got, tt.n)
		}
	}
}
