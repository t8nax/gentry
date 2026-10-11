//go:build windows

package checks

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree is a command with the processes it starts: a job object that ends
// them all when it is terminated or closed. The command starts suspended and
// runs only once it is in the job, so a process it starts at once is in the
// job too.
type tree struct {
	cmd *exec.Cmd
	job windows.Handle
}

func newTree(cmd *exec.Cmd) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return &tree{cmd: cmd}
}

// start starts the command in a new job.
func (t *tree) start() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	t.job = job
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return err
	}
	if err := t.cmd.Start(); err != nil {
		return err
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(t.cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, p)
		if err == nil {
			err = resume(p)
		}
		windows.CloseHandle(p)
	}
	if err != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
		return err
	}
	return nil
}

// kill ends the processes of the job.
func (t *tree) kill() error {
	if t.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(t.job, 1)
}

// close closes the job, which ends the processes left in it.
func (t *tree) close() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}

// ntResumeProcess resumes all the threads of a process. The process has one
// thread when it starts suspended, but Go does not keep its handle.
var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// resume resumes the suspended process p.
func resume(p windows.Handle) error {
	if err := ntResumeProcess.Find(); err != nil {
		return err
	}
	if status, _, _ := ntResumeProcess.Call(uintptr(p)); status != 0 {
		return windows.NTStatus(status)
	}
	return nil
}
