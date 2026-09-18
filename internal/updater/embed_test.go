// T604c · 公钥嵌入单测：EmbeddedPublicKey 解析出 32 字节 Ed25519 公钥
package updater

import (
	"crypto/ed25519"
	"testing"
)

func TestPublicKey_Embedded(t *testing.T) {
	pub := PublicKey()
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("嵌入公钥应为 %d 字节，实得 %d", ed25519.PublicKeySize, len(pub))
	}
}
