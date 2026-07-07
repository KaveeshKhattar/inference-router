package prepare

// TokenizePreparer splits the prompt into fixed-size token blocks using the
// same chars/4 approximation as the router's load estimator.
type TokenizePreparer struct {
	BlockSize int
}

func NewTokenizePreparer(blockSize int) *TokenizePreparer {
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}
	return &TokenizePreparer{BlockSize: blockSize}
}

func (t *TokenizePreparer) Prepare(ctx *RequestContext) error {
	if ctx.PromptText == "" {
		ctx.TokenBlocks = nil
		return nil
	}

	charsPerBlock := t.BlockSize * CharsPerToken
	text := ctx.PromptText
	blocks := make([]string, 0, (len(text)+charsPerBlock-1)/charsPerBlock)

	for start := 0; start < len(text); start += charsPerBlock {
		end := start + charsPerBlock
		if end > len(text) {
			end = len(text)
		}
		blocks = append(blocks, text[start:end])
	}

	ctx.TokenBlocks = blocks
	return nil
}
