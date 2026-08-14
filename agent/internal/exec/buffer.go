package exec

type limitedBuffer struct {
	data      []byte
	max       int
	truncated bool
}

func newLimitedBuffer(max int) *limitedBuffer { return &limitedBuffer{max: max} }
func (b *limitedBuffer) Write(p []byte) (int, error) {
	available := b.max - len(b.data)
	if available < 0 {
		available = 0
	}
	if len(p) > available {
		b.truncated = true
	}
	if available > 0 {
		if available > len(p) {
			available = len(p)
		}
		b.data = append(b.data, p[:available]...)
	}
	return len(p), nil
}
func (b *limitedBuffer) String() string { return string(b.data) }
