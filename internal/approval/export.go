package approval

const MaxPreviewBytes = maxPreviewBytes

func (p *Prompt) SetCode(code func() (string, error)) {
	p.code = code
}
