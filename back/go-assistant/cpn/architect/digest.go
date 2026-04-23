package architect

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// matchSetDigest computes a stable sha256 digest over (qualified_name, sorted
// hashtags) tuples, per the contract in cpn.ToolMatchSet. Moved here to keep
// the retriever file focused on ranking logic.
func matchSetDigest(set cpn.ToolMatchSet) string {
	if len(set.Matches) == 0 {
		return ""
	}
	h := sha256.New()
	for i, m := range set.Matches {
		if i > 0 {
			h.Write([]byte{0x1E}) // record separator
		}
		h.Write([]byte(m.QualifiedName))
		h.Write([]byte{0x1F}) // unit separator
		h.Write([]byte(strings.Join(m.Hashtags, ",")))
	}
	return hex.EncodeToString(h.Sum(nil))
}
