package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/solusio/import-vmware/goroutine"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Commander interface {
	Build(command string, args ...string) Builder
}

type Builder interface {
	WithContext(context.Context) Builder
	WithArgs(...string) Builder
	WithEnv(...string) Builder
	WithStdIn(io.Reader) Builder
	WithStdOut(io.Writer) Builder
	WithNoInfoLog() Builder
	WithIgnoreExitCodes(codes ...int) Builder
	Exec() error
}

type StdCommander struct {
}

var DefaultCommander = &StdCommander{}

func (*StdCommander) Build(command string, args ...string) Builder {
	return &StdCommandBuilder{
		command:  command,
		args:     args,
		logInfo:  log.Println,
		logInfof: log.Printf,
	}
}

type StdCommandBuilder struct {
	ctx             context.Context
	env             []string
	command         string
	args            []string
	stdin           io.Reader
	stdout          io.Writer
	logInfo         func(...interface{})
	logInfof        func(string, ...interface{})
	ignoreExitCodes []int
}

func (c *StdCommandBuilder) WithContext(ctx context.Context) Builder {
	c.ctx = ctx
	return c
}

func (c *StdCommandBuilder) WithArgs(args ...string) Builder {
	c.args = args
	return c
}

func (c *StdCommandBuilder) WithEnv(env ...string) Builder {
	c.env = env
	return c
}

func (c *StdCommandBuilder) WithStdIn(r io.Reader) Builder {
	c.stdin = r
	return c
}

func (c *StdCommandBuilder) WithStdOut(w io.Writer) Builder {
	c.stdout = w
	return c
}

func (c *StdCommandBuilder) WithNoInfoLog() Builder {
	c.logInfo = func(...interface{}) {}
	c.logInfof = func(string, ...interface{}) {}
	return c
}

func (c *StdCommandBuilder) WithIgnoreExitCodes(codes ...int) Builder {
	c.ignoreExitCodes = codes
	return c
}

func (c *StdCommandBuilder) Exec() error { //nolint:gocyclo // Pretty readable though.
	cmdStr := fmt.Sprintf("%s %s", c.command, strings.Join(c.args, " "))

	start := time.Now()
	c.logInfof("Start executing: %s", cmdStr)
	defer func() {
		c.logInfof("Command %s executed within %s", cmdStr, time.Since(start))
	}()

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	cmd := exec.CommandContext(c.ctx, c.command, c.args...) //nolint:gosec // It's okay to execute subprocess with args.

	cmd.Env = append(cmd.Env, c.env...)

	if c.stdin != nil {
		cmd.Stdin = c.stdin
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("get stdout pipe %q: %w", cmdStr, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("get stderr pipe: %q: %w", cmdStr, err)
	}

	// Start the command after having set up the pipe.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", cmdStr, err)
	}

	// Reading from stdout and stderr is separated because "vzpkg create image" hangs when multireader was used.
	var combinedOutput []string
	var wg sync.WaitGroup
	wg.Add(2)
	stdoutScanner := bufio.NewScanner(stdout)
	stderrScanner := bufio.NewScanner(stderr)
	// read command's stdout and stderr line by line
	goroutine.Run(func() {
		for stdoutScanner.Scan() {
			combinedOutput = append(combinedOutput, stdoutScanner.Text())
			c.logInfo(cmdStr, stdoutScanner.Text())
			if c.stdout != nil {
				if _, err := c.stdout.Write(stdoutScanner.Bytes()); err != nil {
					c.logInfo(err)
				}
			}
		}

		if err := stdoutScanner.Err(); err != nil {
			c.logInfo(err)
		}

		wg.Done()
	})

	goroutine.Run(func() {
		for stderrScanner.Scan() {
			combinedOutput = append(combinedOutput, stderrScanner.Text())
			c.logInfo(cmdStr, stderrScanner.Text())
		}

		if err := stderrScanner.Err(); err != nil {
			c.logInfo(err)
		}

		wg.Done()
	})

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if strings.Contains(err.Error(), "signal: killed") {
			err = fmt.Errorf("original error %s: %w", err.Error(), context.DeadlineExceeded)
		}

		if isValidExitCode(err, c.ignoreExitCodes) {
			return nil
		}

		return fmt.Errorf("failed to execute %s stdout %s: %w",
			cmdStr,
			combinedOutput,
			err)
	}

	return c.ctx.Err()
}

func isValidExitCode(err error, validExitCodes []int) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || len(validExitCodes) == 0 {
		return false
	}

	actual := exitErr.ExitCode()
	for _, expected := range validExitCodes {
		if expected == actual {
			return true
		}
	}
	return false
}
