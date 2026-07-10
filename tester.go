package main

import (
	"bytes"
	"log"
	"os/exec"
)

type VerificationReport struct {
	Success   bool
	OutputLog string
}

// VerifyWorkspaceIntegrity fires localized unit testing procedures directly inside the target repository context
func (ai *AgentIntern) VerifyWorkspaceIntegrity() (VerificationReport, error) {
	log.Println("[Testing Gateway] 🧪 Instantiating execution test runs (bun test)...")

	// Set up command execution pipeline to fire inside your flights-scanner folder space
	var outputBuffer bytes.Buffer
	testCmd := exec.Command("bun", "test") // Switch seamlessly to 'npm', 'yarn', or 'vitest' commands if needed
	testCmd.Dir = ai.TargetDir
	testCmd.Stdout = &outputBuffer
	testCmd.Stderr = &outputBuffer

	// Fire the test suite sequence
	err := testCmd.Run()

	report := VerificationReport{
		Success:   err == nil,
		OutputLog: outputBuffer.String(),
	}

	if !report.Success {
		log.Println("[Testing Gateway] ⚠️ Verification fail traces caught inside feature runtime matrix.")
	} else {
		log.Println("[Testing Gateway] ✨ Perfect pass metrics cleared across active code modules!")
	}

	return report, nil
}
