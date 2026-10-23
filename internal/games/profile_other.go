//go:build !linux

package games

// 其他系统没有 systemd scope 与 cgroup，不采集硬件画像。
func newProfileSampler(int, string) *profileSampler { return nil }

func (s *profileSampler) sample() {}
