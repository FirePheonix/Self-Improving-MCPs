package mcp

// Close ends the upstream session.
func (u *StdioUpstream) Close() error {
	if u == nil || u.session == nil {
		return nil
	}
	err := u.session.Close()
	u.session = nil
	return wrapUpstream("close", "", err)
}
