package prepare

// Preparer mutates ctx in place. Stages run in registration order.
type Preparer interface {
	Prepare(ctx *RequestContext) error
}

// Pipeline chains preparers into a single Prepare stage.
type Pipeline struct {
	stages []Preparer
}

func NewPipeline(stages ...Preparer) *Pipeline {
	return &Pipeline{stages: stages}
}

func (p *Pipeline) Prepare(ctx *RequestContext) error {
	for _, stage := range p.stages {
		if err := stage.Prepare(ctx); err != nil {
			return err
		}
	}
	return nil
}

// DefaultBlockSize matches the simulator KV-cache block assumption (16 tokens).
const DefaultBlockSize = 16

// CharsPerToken matches the router's prompt token estimate (proxy/tokens.go).
const CharsPerToken = 4

// NewDefaultPipeline returns Tokenize → BlockHash for the standard Layer 8 path.
func NewDefaultPipeline() *Pipeline {
	return NewPipeline(
		NewTokenizePreparer(DefaultBlockSize),
		NewBlockHashPreparer(),
	)
}
