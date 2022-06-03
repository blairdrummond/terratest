package k8s

import (
	"bytes"
	"fmt"

	"github.com/gruntwork-io/terratest/modules/testing"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/gruntwork-io/terratest/modules/logger"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	executil "k8s.io/client-go/util/exec"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
)

type CommandOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Execute a command in a pod, returning stdout, stderr, and the exit code. The error will be non-nil if the command had a non-zero exit code. (It will also be non-nil if another error occurs)
func ExecE(t testing.TestingT, options *KubectlOptions, podName string, containerName string, command []string) (CommandOutput, error) {
	// Reference: https://github.com/kubernetes/kubectl/blob/7fa5b495e3929467e27f507f36bd27f235316d27/pkg/cmd/exec/exec.go#L289

	config, err := GetKubernetesRestConfigClientE(t, options)
	if err != nil {
		return CommandOutput{}, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return CommandOutput{}, err
	}

	// Get the pod, validate it
	pod, err := GetPodE(t, options, podName)
	if err != nil {
		return CommandOutput{}, err
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return CommandOutput{}, fmt.Errorf("cannot exec into a container in a completed pod; current phase is %s", pod.Status.Phase)
	}

	// Get the containerName if not set
	if len(containerName) == 0 {
		var warn bytes.Buffer
		container, err := podcmd.FindOrDefaultContainerByName(pod, containerName, false, &warn)
		warnStr := warn.String()
		if warnStr != "" {
			logger.Logf(t, "INFO: %s", warnStr)
		}
		if err != nil {
			return CommandOutput{}, err
		}
		containerName = container.Name
	}

	// Set up the exec request
	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", containerName).
		VersionedParams(&corev1.PodExecOptions{
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
			Container: containerName,
			Command:   command,
		}, scheme.ParameterCodec)

	// Execute and recieve output
	execreq, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		panic(err.Error())
	}

	var stdout, stderr bytes.Buffer
	err = execreq.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	// Attempt to get the exit code, fallback to -1
	var exitcode int
	if err == nil {
		exitcode = 0
	} else {
		errcode, ok := err.(*executil.CodeExitError)
		if ok {
			exitcode = errcode.Code
		} else {
			logger.Logf(t, "WARNING: Non-exit code error: %s", err.Error())
			exitcode = -1
		}
	}

	return CommandOutput{stdout.String(), stderr.String(), exitcode}, err
}

func Exec(t testing.TestingT, options *KubectlOptions, podName string, containerName string, command []string) CommandOutput {
	output, err := ExecE(t, options, podName, containerName, command)
	require.NoError(t, err)
	return output
}
