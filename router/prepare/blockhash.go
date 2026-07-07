package prepare

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// BlockHashPreparer computes a cumulative hash chain over TokenBlocks.
// Block N's hash incorporates block N-1's hash, so two requests only match
// on block N when their entire prefix through N is identical.
type BlockHashPreparer struct{}

func NewBlockHashPreparer() *BlockHashPreparer {
	return &BlockHashPreparer{}
}

func (b *BlockHashPreparer) Prepare(ctx *RequestContext) error {
	if len(ctx.TokenBlocks) == 0 {
		ctx.BlockHashes = nil
		return nil
	}

	hashes := make([]BlockHash, len(ctx.TokenBlocks))
	var prev BlockHash

	for i, block := range ctx.TokenBlocks {
		h := sha256.New()
		if i > 0 {
			h.Write(prev[:])
		}
		h.Write([]byte(block))
		copy(hashes[i][:], h.Sum(nil))
		prev = hashes[i]
	}

	ctx.BlockHashes = hashes
	return nil
}

// FormatBlockHash returns a short hex prefix suitable for logs.
func FormatBlockHash(h BlockHash) string {
	return hex.EncodeToString(h[:4])
}

// FormatBlockHashChain renders the full chain for debug output.
func FormatBlockHashChain(hashes []BlockHash) string {
	if len(hashes) == 0 {
		return "[]"
	}
	parts := make([]string, len(hashes))
	for i, h := range hashes {
		parts[i] = FormatBlockHash(h)
	}
	return fmt.Sprintf("[%s]", joinComma(parts))
}

func joinComma(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
