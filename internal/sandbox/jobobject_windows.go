//go:build windows

package sandbox

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobSandbox is the Windows CmdSandbox implementation that wraps a Job Object.
// It ensures child processes are terminated when the parent exits (even abruptly),
// providing process-tree lifecycle management.
type jobSandbox struct {
	handle windows.Handle
}

// configureCmdOS creates a Windows Job Object with kill-on-close for the
// command. In read-only and workspace-write modes, the Job Object is created;
// in full-access mode, a no-op sandbox is returned.
func configureCmdOS(spec Spec, cmd *exec.Cmd) (CmdSandbox, error) {
	mode := spec.EffectiveMode()
	if mode == SandboxFullAccess {
		return noopSandbox{}, nil
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set job object info: %w", err)
	}

	return &jobSandbox{handle: job}, nil
}

// AssignProcess assigns the process in cmd to the Job Object. This must be
// called after cmd.Start() succeeds and before cmd.Wait() is called.
func (s *jobSandbox) AssignProcess(cmd *exec.Cmd) error {
	if s.handle == 0 || cmd == nil || cmd.Process == nil {
		return nil
	}
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open process for job assignment: %w", err)
	}
	defer func() { _ = windows.CloseHandle(procHandle) }()

	if err := windows.AssignProcessToJobObject(s.handle, procHandle); err != nil {
		return fmt.Errorf("assign process to job object: %w", err)
	}
	return nil
}

// Close releases the Job Object handle. If the job was created with
// KILL_ON_JOB_CLOSE, closing the handle terminates all processes in the job.
func (s *jobSandbox) Close() error {
	if s.handle == 0 {
		return nil
	}
	h := s.handle
	s.handle = 0
	return windows.CloseHandle(h)
}

// Kill terminates all processes in the Job Object by calling
// TerminateJobObject, then closes the handle.
func (s *jobSandbox) Kill() error {
	if s.handle == 0 {
		return nil
	}
	err := windows.TerminateJobObject(s.handle, 1)
	_ = s.Close()
	return err
}

// SeccompFD returns 0 on Windows (seccomp is Linux-only).
func (s *jobSandbox) SeccompFD() int { return 0 }
