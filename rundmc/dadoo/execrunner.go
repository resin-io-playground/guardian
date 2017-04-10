package dadoo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/guardian/rundmc/runrunc"
	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/gunk/command_runner"
	"github.com/kr/logfmt"
)

//go:generate counterfeiter . PidGetter
type PidGetter interface {
	Pid(pidFilePath string) (int, error)
}

type ExecRunner struct {
	dadooPath     string
	runcPath      string
	processIDGen  runrunc.UidGenerator
	pidGetter     PidGetter
	commandRunner command_runner.CommandRunner
}

func NewExecRunner(dadooPath, runcPath string, processIDGen runrunc.UidGenerator, pidGetter PidGetter, commandRunner command_runner.CommandRunner) *ExecRunner {
	return &ExecRunner{
		dadooPath:     dadooPath,
		runcPath:      runcPath,
		processIDGen:  processIDGen,
		pidGetter:     pidGetter,
		commandRunner: commandRunner,
	}
}

func (d *ExecRunner) Run(log lager.Logger, processID string, spec *runrunc.PreparedSpec, bundlePath, processesPath, handle string, tty *garden.TTYSpec, pio garden.ProcessIO) (p garden.Process, theErr error) {
	log = log.Session("execrunner")

	log.Info("start")
	defer log.Info("done")

	if processID == "" {
		processID = d.processIDGen.Generate()
	}
	processPath := filepath.Join(processesPath, processID)
	_, err := os.Stat(processPath)
	if err == nil {
		return nil, errors.New(fmt.Sprintf("process ID '%s' already in use", processID))
	}

	if err := os.MkdirAll(processPath, 0700); err != nil {
		return nil, err
	}

	fd3r, fd3w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	logr, logw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	syncr, syncw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	defer fd3r.Close()
	defer logr.Close()
	defer syncr.Close()

	process := newProcess(processID, processPath, filepath.Join(processPath, "pidfile"), d.pidGetter)
	process.mkfifos(spec.HostUID, spec.HostGID)
	if err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if tty != nil {
		cmd = exec.Command(d.dadooPath, "-tty", "exec", d.runcPath, processPath, handle)
	} else {
		cmd = exec.Command(d.dadooPath, "exec", d.runcPath, processPath, handle)
	}

	dadooLogFilePath := filepath.Join(bundlePath, fmt.Sprintf("dadoo.%s.log", processID))
	dadooLogFile, err := os.Create(dadooLogFilePath)
	if err != nil {
		return nil, err
	}
	defer dadooLogFile.Close()
	cmd.Stdout = dadooLogFile
	cmd.Stderr = dadooLogFile

	cmd.ExtraFiles = []*os.File{
		fd3w,
		logw,
		syncw,
	}

	encodedSpec, err := json.Marshal(spec.Process)
	if err != nil {
		return nil, err // this could *almost* be a panic: a valid spec should always encode (but out of caution we'll error)
	}

	cmd.Stdin = bytes.NewReader(encodedSpec)
	if err := d.commandRunner.Start(cmd); err != nil {
		return nil, err
	}
	go func() {
		// wait on spawned process to avoid zombies
		d.commandRunner.Wait(cmd)
		if copyErr := copyDadooLogsToGuardianLogger(dadooLogFilePath, log); copyErr != nil {
			log.Error("reading-dadoo-log-file", copyErr)
		}
	}()

	fd3w.Close()
	logw.Close()
	syncw.Close()

	stdin, stdout, stderr, err := process.openPipes()
	if err != nil {
		return nil, err
	}

	syncMsg := make([]byte, 1)
	_, err = syncr.Read(syncMsg)
	if err != nil {
		return nil, err
	}

	process.streamData(pio, stdin, stdout, stderr)
	defer func() {
		theErr = processLogs(log, logr, theErr, "runc", "runc exec")
	}()

	log.Info("read-exit-fd")
	runcExitStatus := make([]byte, 1)
	bytesRead, err := fd3r.Read(runcExitStatus)
	if bytesRead == 0 || err != nil {
		return nil, errors.New("failed to read runc exit code")
	}
	log.Info("runc-exit-status", lager.Data{"status": runcExitStatus[0]})
	if runcExitStatus[0] != 0 {
		return nil, fmt.Errorf("exit status %d", runcExitStatus[0])
	}

	return process, nil
}

func (d *ExecRunner) Attach(log lager.Logger, processID string, io garden.ProcessIO, processesPath string) (garden.Process, error) {
	processPath := filepath.Join(processesPath, processID)
	process := newProcess(processID, processPath, filepath.Join(processPath, "pidfile"), d.pidGetter)
	if err := process.attach(io); err != nil {
		return nil, err
	}

	return process, nil
}

type osSignal garden.Signal

func (s osSignal) OsSignal() syscall.Signal {
	switch garden.Signal(s) {
	case garden.SignalTerminate:
		return syscall.SIGTERM
	default:
		return syscall.SIGKILL
	}
}

type process struct {
	id                                           string
	stdin, stdout, stderr, exit, winsz, exitcode string
	ioWg                                         *sync.WaitGroup
	winszCh                                      chan garden.WindowSize
	cleanup                                      func() error

	*signaller
}

func newProcess(id, dir string, pidFilePath string, pidGetter PidGetter) *process {
	stdin := filepath.Join(dir, "stdin")
	stdout := filepath.Join(dir, "stdout")
	stderr := filepath.Join(dir, "stderr")
	winsz := filepath.Join(dir, "winsz")
	exit := filepath.Join(dir, "exit")
	exitcode := filepath.Join(dir, "exitcode")

	return &process{
		id:       id,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		winsz:    winsz,
		exit:     exit,
		exitcode: exitcode,
		ioWg:     &sync.WaitGroup{},
		winszCh:  make(chan garden.WindowSize, 5),
		cleanup: func() error {
			return os.RemoveAll(dir)
		},
		signaller: &signaller{
			pidFilePath: pidFilePath,
			pidGetter:   pidGetter,
		},
	}
}

func (p *process) ID() string {
	return p.id
}

func (p *process) mkfifos(hostUid, hostGid int) error {
	for _, pipe := range []string{p.stdin, p.stdout, p.stderr, p.winsz, p.exit} {
		if err := syscall.Mkfifo(pipe, 0600); err != nil {
			return err
		}
		if err := os.Chown(pipe, hostUid, hostGid); err != nil {
			return err
		}
	}

	return nil
}

func (p process) openPipes() (stdin, stdout, stderr *os.File, err error) {
	stdin, err = os.OpenFile(p.stdin, os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err = openNonBlocking(p.stdout)
	if err != nil {
		return nil, nil, nil, err
	}
	stderr, err = openNonBlocking(p.stderr)
	if err != nil {
		return nil, nil, nil, err
	}

	return stdin, stdout, stderr, nil
}

func openNonBlocking(fileName string) (*os.File, error) {
	file, err := os.OpenFile(fileName, os.O_RDONLY|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.SetNonblock(int(file.Fd()), false); err != nil {
		return nil, err
	}
	return file, nil
}

func (p process) streamData(pio garden.ProcessIO, stdin, stdout, stderr *os.File) {
	p.streamInput(pio.Stdin, stdin)
	p.streamOutput(pio.Stdout, stdout)
	p.streamOutput(pio.Stderr, stderr)
}

func (p process) streamInput(processReader io.Reader, input *os.File) {
	if processReader == nil {
		input.Close()
		return
	}

	go func() {
		io.Copy(input, processReader)
		input.Close()
	}()
}

func (p process) streamOutput(processWriter io.Writer, output *os.File) {
	if processWriter == nil {
		output.Close()
		return
	}

	p.ioWg.Add(1)
	go func() {
		io.Copy(processWriter, output)
		output.Close()
		p.ioWg.Done()
	}()
}

func (p process) attach(pio garden.ProcessIO) error {
	stdin, stdout, stderr, err := p.openPipes()
	if err != nil {
		return err
	}

	p.streamData(pio, stdin, stdout, stderr)

	return nil
}

func (p process) ExitStatus() chan garden.ProcessStatus {
	c := make(chan garden.ProcessStatus, 1)
	go func() {
		// open non-blocking incase exit pipe is already closed
		exit, err := openNonBlocking(p.exit)
		if err != nil {
			c <- garden.ProcessStatus{Code: 1, Err: err}
		}
		defer exit.Close()

		buf := make([]byte, 1)
		exit.Read(buf)

		p.ioWg.Wait()

		if _, err := os.Stat(p.exitcode); os.IsNotExist(err) {
			c <- garden.ProcessStatus{Code: 1, Err: fmt.Errorf("could not find the exitcode file for the process: %s", err.Error())}
		}

		exitcode, err := ioutil.ReadFile(p.exitcode)
		if err != nil {
			c <- garden.ProcessStatus{Code: 1, Err: err}
		}

		if len(exitcode) == 0 {
			c <- garden.ProcessStatus{Code: 1, Err: fmt.Errorf("the exitcode file is empty")}
		}

		code, err := strconv.Atoi(string(exitcode))
		if err != nil {
			c <- garden.ProcessStatus{Code: 1, Err: fmt.Errorf("failed to parse exit code: %s", err.Error())}
		}
		c <- garden.ProcessStatus{Code: code, Err: nil}
	}()
	return c
}

func (p process) Wait() (int, error) {
	ret := <-p.ExitStatus()

	if err := p.cleanup(); err != nil {
		return 1, err
	}

	return ret.Code, ret.Err
}

func (p process) SetTTY(spec garden.TTYSpec) error {
	if spec.WindowSize == nil {
		return nil
	}

	winSize, err := os.OpenFile(p.winsz, os.O_WRONLY|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}

	defer winSize.Close()
	return json.NewEncoder(winSize).Encode(spec.WindowSize)
}

func processLogs(log lager.Logger, logs io.Reader, err error, logTag, logLinePrefix string) error {
	buff, readErr := ioutil.ReadAll(logs)

	if readErr != nil {
		return fmt.Errorf("start: read log file: %s", readErr)
	}

	forwardLogsToLager(log, buff, logTag)

	if err != nil {
		return wrapWithErrorFromLog(log, err, buff, logLinePrefix)
	}

	return nil
}

func forwardLogsToLager(log lager.Logger, buff []byte, tag string) {
	for _, logLine := range strings.Split(string(buff), "\n") {
		parsedLogLine := struct{ Msg string }{}
		if err := logfmt.Unmarshal([]byte(logLine), &parsedLogLine); err == nil {
			log.Debug(tag, lager.Data{
				"message": parsedLogLine.Msg,
			})
		}
	}
}

func wrapWithErrorFromLog(log lager.Logger, originalError error, buff []byte, logLinePrefix string) error {
	parsedLogLine := struct{ Msg string }{}
	logfmt.Unmarshal(buff, &parsedLogLine)
	return fmt.Errorf("%s: %s: %s", logLinePrefix, originalError, parsedLogLine.Msg)
}

type signaller struct {
	pidFilePath string
	pidGetter   PidGetter
}

func (s *signaller) Signal(signal garden.Signal) error {
	pid, err := s.pidGetter.Pid(s.pidFilePath)
	if err != nil {
		return errors.New(fmt.Sprintf("fetching-pid: %s", err))
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return errors.New(fmt.Sprintf("finding-process: %s", err))
	}

	return process.Signal(osSignal(signal).OsSignal())
}

func copyDadooLogsToGuardianLogger(dadooLogFilePath string, logger lager.Logger) error {
	dadooLogFileInfo, err := os.Stat(dadooLogFilePath)
	if err != nil {
		return fmt.Errorf("stating dadoo log file: %s", err)
	}
	defer func() {
		if err := os.Remove(dadooLogFilePath); err != nil {
			logger.Error("removing-dadoo-log-file", err)
		}
	}()
	if dadooLogFileInfo.Size() == 0 {
		return nil
	}

	dadooLogs, err := ioutil.ReadFile(dadooLogFilePath)
	if err != nil {
		return fmt.Errorf("failed to read dadoo log file: %s", err)
	}
	logger.Debug("dadoo", lager.Data{"message": string(dadooLogs)})
	return nil
}
