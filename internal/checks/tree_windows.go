//go:build windows

package checks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree is a command with the processes it starts: a job object that ends
// them all when it is terminated or closed. The command starts suspended and
// runs only once it is in the job, so a process it starts at once is in the
// job too. The job of a command with a mark is named after the mark, so that
// End finds it from another process.
type tree struct {
	cmd   *exec.Cmd
	mark  string
	ready func() error
	job   windows.Handle
}

func newTree(cmd *exec.Cmd, mark string, ready func() error) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return &tree{cmd: cmd, mark: mark, ready: ready}
}

// start starts the command in a new job; the command runs only if ready, if
// set, allows it once the command is in the job. An error of the start of
// the program is a *startError.
func (t *tree) start() error {
	var name *uint16
	if t.mark != "" {
		var err error
		if name, err = windows.UTF16PtrFromString(jobName(t.mark)); err != nil {
			return err
		}
	}
	job, err := windows.CreateJobObject(nil, name)
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
		return &startError{err}
	}
	if startedHook != nil {
		startedHook()
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(t.cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, p)
		if err == nil && t.ready != nil {
			err = t.ready()
		}
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

// startedHook, if set, is called after the command starts and before it is
// put in its job; tests set it.
var startedHook func()

// kill ends the command and the processes of the job. The command is ended
// by itself too: until it is in the job, the job does not end it, and the
// command would run once it is put there and resumed. It starts no process
// of its own before then, as it is suspended.
func (t *tree) kill() error {
	var err error
	if t.cmd.Process != nil {
		if e := t.cmd.Process.Kill(); e != nil && !errors.Is(e, os.ErrProcessDone) {
			err = e
		}
	}
	if t.job != 0 {
		if e := windows.TerminateJobObject(t.job, 1); e != nil {
			err = e
		}
	}
	return err
}

// close closes the job, which ends the processes left in it.
func (t *tree) close() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}

// jobName is the name of the job of a command with mark: in the namespace of
// the session of the user, by a hash of the path of the mark.
func jobName(mark string) string {
	if abs, err := filepath.Abs(mark); err == nil {
		mark = abs
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(mark))))
	return `Local\gentry-checks-` + hex.EncodeToString(sum[:16])
}

// openJobObject opens a job object by its name.
var openJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

// jobObjectTerminate is the right to terminate a job object.
const jobObjectTerminate = 0x0008

// End ends the command that runs with mark, in this process or another, with
// every process it started: it terminates the job named after the mark. If
// no command runs with mark, End does nothing.
func End(mark string) error {
	name, err := windows.UTF16PtrFromString(jobName(mark))
	if err != nil {
		return err
	}
	if err := openJobObject.Find(); err != nil {
		return err
	}
	h, _, e := openJobObject.Call(jobObjectTerminate, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		return e
	}
	defer windows.CloseHandle(windows.Handle(h))
	return windows.TerminateJobObject(windows.Handle(h), 1)
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
