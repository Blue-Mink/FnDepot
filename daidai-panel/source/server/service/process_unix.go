//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func setPgid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func SetPgid(cmd *exec.Cmd) {
	setPgid(cmd)
}

func killGroup(p *os.Process) {
	syscall.Kill(-p.Pid, syscall.SIGKILL)
}

func killGroupByPid(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}

// describeTerminationSignal 从 cmd.Wait() 的错误里判断「这个进程是不是被信号杀掉的」，
// 是的话返回一个人能看懂的描述（例如 killed(9) / terminated(15)），否则返回空串。
//
// 为什么需要它：进程被信号终止时 exec.ExitError.ExitCode() 恒为 -1，
// 脚本日志里只剩一句「退出码 -1」，用户没法区分是 OOM Killer 杀的、面板停止的，还是外部 kill 的。
// runSingleCommand 拿它来补一行可见提示（调用点有更详细的说明）。
//
// 正常退出（哪怕退出码非 0）、以及 waitErr 不是 *exec.ExitError 的情况，都返回空串，
// 这样调用方一个 != "" 判断就够，不用再关心平台差异。
func describeTerminationSignal(waitErr error) string {
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return ""
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	sig := status.Signal()
	return fmt.Sprintf("%s(%d)", sig.String(), int(sig))
}
