//go:build windows

package service

import (
	"os"
	"os/exec"
)

func setPgid(cmd *exec.Cmd) {
}

func SetPgid(cmd *exec.Cmd) {
}

func killGroup(p *os.Process) {
}

func killGroupByPid(pid int) {
}

// describeTerminationSignal 在 Windows 上恒返回空串。
// Windows 没有 POSIX 信号那套语义，进程被 TerminateProcess 结束时拿到的是一个普通退出码
// （不会像 Unix 那样退化成 -1），所以不存在「退出码 -1 但没解释」的现象，
// runSingleCommand 里那行提示自然也就不会打出来。
// 保留同名同签名的空实现，只是为了让调用方不用写平台分支。
func describeTerminationSignal(waitErr error) string {
	return ""
}
