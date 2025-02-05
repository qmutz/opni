package b2mac_test

import (
	"crypto/ed25519"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/opni/pkg/b2mac"
)

var _ = Describe("MAC", Label("unit"), func() {
	It("should correctly generate a MAC", func() {
		_, key, err := ed25519.GenerateKey(nil)
		Expect(err).NotTo(HaveOccurred())
		tenantID := []byte(uuid.NewString())
		payload := []byte("test")
		mac, err := b2mac.New512(tenantID, uuid.New(), payload, key)
		Expect(err).NotTo(HaveOccurred())
		Expect(mac).To(HaveLen(512 / 8))
	})
	It("should error if the key is the wrong length", func() {
		key := make([]byte, 65)
		tenantID := []byte(uuid.NewString())
		payload := []byte("test")
		nonce := uuid.New()
		_, err := b2mac.New512(tenantID, nonce, payload, key)
		Expect(err).To(MatchError("blake2b: invalid key size"))
		err = b2mac.Verify([]byte(""), tenantID, nonce, payload, key)
		Expect(err).To(MatchError("blake2b: invalid key size"))
	})
	It("should correctly verify MACs", func() {
		_, key, err := ed25519.GenerateKey(nil)
		Expect(err).NotTo(HaveOccurred())
		tenantID := []byte(uuid.NewString())
		payload := []byte("test")
		nonce := uuid.New()
		mac, err := b2mac.New512(tenantID, nonce, payload, key)
		Expect(err).NotTo(HaveOccurred())
		err = b2mac.Verify(mac, tenantID, nonce, payload, key)
		Expect(err).NotTo(HaveOccurred())
		_, wrongKey, err := ed25519.GenerateKey(nil)
		Expect(err).NotTo(HaveOccurred())
		err = b2mac.Verify(mac, tenantID, nonce, payload, wrongKey)
		Expect(err).To(MatchError("verification failed"))
	})
})
