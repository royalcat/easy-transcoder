package transcoding

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// executeFFmpegCommand is a helper function that runs an ffmpeg command and returns stdout output
func executeFFmpegCommand(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("ffmpeg command failed with exit code %d: %w\nDetails: %s",
				exitErr.ExitCode(), err, stderr.String())
		}
		return "", fmt.Errorf("ffmpeg command execution error: %w\nDetails: %s", err, stderr.String())
	}

	return stderr.String(), nil
}

func CalculateVMAF(ctx context.Context, reference, distorted string) (float64, error) {
	args := []string{
		"-hwaccel", "auto",
		"-i", distorted,
		"-i", reference,
		"-filter_complex", "libvmaf",
		"-f", "null", "-",
	}

	output, err := executeFFmpegCommand(ctx, args)
	if err != nil {
		return 0, err
	}

	// Parse the output for VMAF score
	var vmafScore float64

	// Look for the VMAF score line
	// Example: "[libvmaf @ 0x1b5b700] VMAF score: 99.055347"
	var re = regexp.MustCompile(`\[libvmaf[^]]*\]\s*VMAF score:\s*(\d+\.\d+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) > 1 {
		vmafScore, err = strconv.ParseFloat(matches[1], 64)
		if err != nil {
			return 0, err
		}
		return vmafScore, nil
	}

	return 0, fmt.Errorf("VMAF score not found in output")
}

func CalculatePSNR(ctx context.Context, reference, distorted string) (float64, error) {
	args := []string{
		"-hwaccel", "auto",
		"-i", distorted,
		"-i", reference,
		"-filter_complex", "psnr",
		"-f", "null", "-",
	}

	output, err := executeFFmpegCommand(ctx, args)
	if err != nil {
		return 0, err
	}

	// Parse the output for PSNR score
	var psnrScore float64

	// Look for the PSNR score line
	// Example: "PSNR y:56.155712 u:62.115347 v:61.487041 average:57.360421 min:53.954091 max:76.377962"
	var re = regexp.MustCompile(`PSNR.*average:(\d+\.\d+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) > 1 {
		psnrScore, err = strconv.ParseFloat(matches[1], 64)
		if err != nil {
			return 0, err
		}
		return psnrScore, nil
	}

	return 0, fmt.Errorf("PSNR score not found in output")
}

func CalculateSSIM(ctx context.Context, reference, distorted string) (float64, error) {
	args := []string{
		"-hwaccel", "auto",
		"-i", distorted,
		"-i", reference,
		"-filter_complex", "ssim",
		"-f", "null", "-",
	}

	output, err := executeFFmpegCommand(ctx, args)
	if err != nil {
		return 0, err
	}

	// Parse the output for SSIM score
	var ssimScore float64

	// Look for the SSIM score line
	// Example: "SSIM Y:0.999334 (31.765328) U:0.999581 (33.779794) V:0.999555 (33.512373) All:0.999412 (32.306001)"
	var re = regexp.MustCompile(`SSIM [^A]*All:(\d+\.\d+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) > 1 {
		ssimScore, err = strconv.ParseFloat(matches[1], 64)
		if err != nil {
			return 0, err
		}
		return ssimScore, nil
	}

	return 0, fmt.Errorf("SSIM score not found in output")
}
