//go:build !windows

package conference

func setConferenceWebViewOutputMuted(muted bool) error {
	return nil
}
