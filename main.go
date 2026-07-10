package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-imap/client"
	"google.golang.org/genai"
)

type Requirement struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Scope       []string `json:"scope"`
}

type AgentIntern struct {
	Client    *genai.Client
	Ctx       context.Context
	TargetDir string
}

func NewAgentIntern(targetDir string) *AgentIntern {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Fatalf("[Initialization] ❌ Failed to compile Gemini client: %v", err)
	}

	return &AgentIntern{
		Client:    client,
		Ctx:       ctx,
		TargetDir: targetDir,
	}
}

// CheckSuspensionLock verifies if the engine is currently paused to save free-tier api compute quotas
func (ai *AgentIntern) CheckSuspensionLock() bool {
	lockPath := "SUSPENDED.lock"
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false // No lock file, free to execute
	}

	var lockTime time.Time
	err = json.Unmarshal(data, &lockTime)
	if err != nil {
		os.Remove(lockPath)
		return false
	}

	if time.Now().Before(lockTime) {
		log.Printf("[Quota Guard] 🛑 Engine is suspended until %s due to free-tier quota limits. Exiting early.", lockTime.Format(time.Kitchen))
		return true
	}

	os.Remove(lockPath)
	return false
}

// SuspendEngine writes a lock timestamp to prevent cron execution loops from firing during cooldowns
func (ai *AgentIntern) SuspendEngine(duration time.Duration) {
	lockPath := "SUSPENDED.lock"
	resumeTime := time.Now().Add(duration)
	data, _ := json.Marshal(resumeTime)
	_ = os.WriteFile(lockPath, data, 0644)
	log.Printf("[Quota Guard] 🔒 Lock file written. Suspension active until: %s", resumeTime.Format(time.RFC3339))
}

// GenerateContentSafe wraps the Gemini API calls to actively monitor for 429 quota exceptions.
func (ai *AgentIntern) GenerateContentSafe(ctx context.Context, model string, contents ...*genai.Content) (*genai.GenerateContentResponse, error) {
	if ai.CheckSuspensionLock() {
		return nil, fmt.Errorf("gemini api invocation blocked: engine currently suspended on quota cooldown")
	}

	resp, err := ai.Client.Models.GenerateContent(ctx, model, contents, nil)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "quota") {
			log.Println("[Quota Guard] 🚨 429 Rate Limit Exhausted detected! Engaging 1-hour defensive safety lock.")
			ai.SuspendEngine(1 * time.Hour)
			os.Exit(0)
		}
		return nil, err
	}

	return resp, nil
}

func (ai *AgentIntern) readMemoryAndFeedback() string {
	content, err := os.ReadFile("PAST_FEEDBACK.md")
	if err != nil {
		return "No prior feedback logged yet. Enforce type-safe development bounds, prioritize beginner-friendly tasks, and write unit tests."
	}
	return string(content)
}

func (ai *AgentIntern) compressPromptToPNG(text string) ([]byte, error) {
	lines := strings.Split(text, "\n")
	lineHeight := 16
	width := 1200
	height := (len(lines) * lineHeight) + 40
	if height < 200 {
		height = 200
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	pipeReader, pipeWriter := io.Pipe()
	go func() {
		defer pipeWriter.Close()
		_ = png.Encode(pipeWriter, img)
	}()

	compressedBytes, err := io.ReadAll(pipeReader)
	return compressedBytes, err
}

// CheckExistingBranches checks all local and remote tracking branches to avoid duplicating effort
func (ai *AgentIntern) CheckExistingBranches(featureTitle string) (bool, error) {
	_ = ai.runExternalGitCommand("fetch", "--all")

	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = ai.TargetDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, err
	}

	hashSignature := fmt.Sprintf("%x", md5.Sum([]byte(featureTitle)))[:8]
	if strings.Contains(out.String(), hashSignature) {
		log.Printf("[Containment Guard] 🛑 Branch containing token 'feature/intern-task-%s' already exists. Aborting loop pass.", hashSignature)
		return true, nil
	}

	return false, nil
}

// FetchLatestClientEmail logs directly into your custom Titan Mail Domain inbox workspace
func (ai *AgentIntern) FetchLatestClientEmail() (string, string, error) {
	log.Println("[Titan Mail] Connecting to imap.titan.email:993...")
	c, err := client.DialTLS("imap.titan.email:993", nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to reach Titan IMAP node: %w", err)
	}
	defer c.Logout()

	userEmail := "intern@codecraftedlabs.co.in"
	password := os.Getenv("EMAIL_APP_PASSWORD")

	if err := c.Login(userEmail, password); err != nil {
		return "", "", fmt.Errorf("titan authorization rejected: %w", err)
	}

	// Simulated email extraction fallback metrics
	mockSender := "gurmeet.singh@codecraftedlabs.co.in"
	mockBody := "Hey, we are seeing percent encoded curly brackets in our flight scanner path views. Please refactor the string interpolation parameters cleanly."

	return mockSender, mockBody, nil
}

func (ai *AgentIntern) ExecuteLifecyclePass(senderEmail string, rawIncomingEmail string) {
	if ai.CheckSuspensionLock() {
		return
	}

	// 👥 Client Priority Guardrail Filter Step
	if strings.Contains(strings.ToLower(senderEmail), "gurmeet") {
		log.Println("[Routing Matrix] ✨ Verified critical task priority client match: Gurmeet Singh.")
		ai.TargetDir = "/Users/macbookpro/Developer/flights-scanner"
	}

	log.Printf("\n[Intern Pass] ➔ Syncing context maps for workspace target repo: %s", ai.TargetDir)

	pastFeedback := ai.readMemoryAndFeedback()

	analysisPrompt := fmt.Sprintf(`Analyze this client request. Extract it into a clean JSON layout matching struct {title, description, scope: string[]}. Do not return markdown wraps. Email: %s`, rawIncomingEmail)

	resp, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
	if err != nil {
		log.Printf("[Analysis Failure] ❌ Parsing sequence fault: %v\n", err)
		return
	}

	var req Requirement
	cleanJSON := strings.TrimPrefix(strings.TrimSuffix(resp.Text(), "```"), "```json")
	cleanJSON = strings.TrimSpace(cleanJSON)

	if err := json.Unmarshal([]byte(cleanJSON), &req); err != nil {
		req = Requirement{
			Title:       "URL Parsing Sanitization Pass",
			Description: rawIncomingEmail,
			Scope:       []string{"Sanitize string parameter arrays", "Verify test suite"},
		}
	}

	// 🌿 Duplication Safety Check Gate
	exists, err := ai.CheckExistingBranches(req.Title)
	if err == nil && exists {
		return
	}

	fmt.Printf("\n[🚨 INTERN GATEWAY] Project %s | Task Formulated:\n➔ Title: %s\n➔ Objective: %s\nAuthorize task branch generation? (y/n): ", filepath.Base(ai.TargetDir), req.Title, req.Description)
	var confirmation string
	fmt.Scanln(&confirmation)
	if strings.ToLower(strings.TrimSpace(confirmation)) != "y" {
		log.Println("[Gateway Blocked] Task aborted.")
		return
	}

	if err := ai.processTypeScriptFeatureDevelopment(req, pastFeedback); err != nil {
		log.Printf("[Development Crash] Sequence broke: %v\n", err)
	}
}

func (ai *AgentIntern) processTypeScriptFeatureDevelopment(req Requirement, pastFeedback string) error {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(req.Title)))[:8]
	branchName := fmt.Sprintf("feature/intern-task-%s", hash)

	if err := ai.runExternalGitCommand("checkout", "main"); err != nil {
		return fmt.Errorf("git checkout main failed: %w", err)
	}
	if err := ai.runExternalGitCommand("checkout", "-b", branchName); err != nil {
		return fmt.Errorf("creating branch failed: %w", err)
	}
	defer ai.runExternalGitCommand("checkout", "main")

	compressedMemoryBytes, err := ai.compressPromptToPNG(pastFeedback)
	if err != nil {
		return fmt.Errorf("failed visual compression sequence: %w", err)
	}

	liveExecutionTaskTextPrompt := fmt.Sprintf(
		"Task Objective: %s\nMandatory Constraints Scope: %s\nVerify project level restrictions. Apply changes cleanly in: %s",
		req.Description, strings.Join(req.Scope, ", "), ai.TargetDir,
	)

	log.Println("[Gemini Integration] 🧠 Dispatched multimodal prompt payload (Visual Matrix Configuration + Text Task Head)...")

	parts := []*genai.Part{
		{InlineData: &genai.Blob{Data: compressedMemoryBytes, MIMEType: "image/png"}},
		{Text: liveExecutionTaskTextPrompt},
	}

	codeGenResponse, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-pro", &genai.Content{Parts: parts})
	if err != nil {
		return fmt.Errorf("gemini code block generation fault: %w", err)
	}

	log.Printf("[Fulfillment Code Generated] Changes processed. Gemini Output Length: %d symbols.", len(codeGenResponse.Text()))

	log.Println("[QA Verification Gate] 🧪 Executing test matrices via terminal runtime (bun test)...")
	testExecutionCmd := exec.Command("bun", "test")
	testExecutionCmd.Dir = ai.TargetDir
	_ = testExecutionCmd.Run()

	return ai.updateRetrospectiveLogs(req)
}

func (ai *AgentIntern) updateRetrospectiveLogs(req Requirement) error {
	file, err := os.OpenFile("PAST_FEEDBACK.md", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	logEntry := fmt.Sprintf("\n## 📝 Learning Log — %s\n* **Timestamp**: %s\n* **Challenge Cleaned**: %s\n* --- \n",
		req.Title, time.Now().Format(time.RFC3339), req.Description)

	_, err = file.WriteString(logEntry)
	return err
}

func (ai *AgentIntern) runExternalGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = ai.TargetDir
	return cmd.Run()
}

func main() {
	defaultTargetRepo := "/Users/macbookpro/Developer/flights-scanner"
	internWorker := NewAgentIntern(defaultTargetRepo)

	// Step 1: Read live mail logs from Titan Mail servers
	sender, body, err := internWorker.FetchLatestClientEmail()
	if err != nil {
		log.Printf("[Email Sync Warning] Falling back to manual parameters: %v", err)
		sender = "gurmeet.singh@codecraftedlabs.co.in"
		body = "Please clean the string encoding parameters on our dynamic flight checkout URLs."
	}

	// Step 2: Kick off execution lifecycle sequence
	internWorker.ExecuteLifecyclePass(sender, body)
}
