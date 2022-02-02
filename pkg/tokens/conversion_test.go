package tokens_test

import (
	"encoding/hex"

	"github.com/kralicky/opni-monitoring/pkg/core"
	"github.com/kralicky/opni-monitoring/pkg/tokens"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
)

var _ = Describe("Conversion", func() {
	Specify("Tokens should convert between API types", func() {
		t := tokens.NewToken()

		bt := t.ToBootstrapToken()
		Expect(bt.TokenID).To(Equal(t.HexID()))
		Expect(bt.Secret).To(Equal(t.HexSecret()))
		Expect(bt.LeaseID).To(Equal(t.Metadata.LeaseID))
		Expect(bt.Ttl).To(Equal(t.Metadata.TTL))

		t2, err := tokens.FromBootstrapToken(bt)
		Expect(err).NotTo(HaveOccurred())

		Expect(t2).To(Equal(t))

		bt2 := t2.ToBootstrapToken()
		Expect(proto.Equal(bt, bt2)).To(BeTrue())
	})
	When("converting from core.BootstrapToken to tokens.Token", func() {
		It("should handle decoding errors", func() {
			bt := &core.BootstrapToken{
				TokenID: "invalid",
				Secret:  hex.EncodeToString([]byte("secret")),
			}
			_, err := tokens.FromBootstrapToken(bt)
			Expect(err).To(HaveOccurred())

			bt = &core.BootstrapToken{
				TokenID: hex.EncodeToString([]byte("id")),
				Secret:  "invalid",
			}
			_, err = tokens.FromBootstrapToken(bt)
			Expect(err).To(HaveOccurred())
		})
	})
})
