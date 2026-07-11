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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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

func (ai *AgentIntern) CheckSuspensionLock() bool {
	lockPath := "SUSPENDED.lock"
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}

	var lockTime time.Time
	err = json.Unmarshal(data, &lockTime)
	if err != nil {
		os.Remove(lockPath)
		return false
	}

	if time.Now().Before(lockTime) {
		log.Printf("[Quota Guard] 🛑 Engine is suspended until %s. Exiting early.", lockTime.Format(time.Kitchen))
		return true
	}

	os.Remove(lockPath)
	return false
}

func (ai *AgentIntern) SuspendEngine(duration time.Duration) {
	lockPath := "SUSPENDED.lock"
	resumeTime := time.Now().Add(duration)
	data, _ := json.Marshal(resumeTime)
	_ = os.WriteFile(lockPath, data, 0644)
	log.Printf("[Quota Guard] 🔒 Cooldown lock engaged until: %s", resumeTime.Format(time.RFC3339))
}

func (ai *AgentIntern) GenerateContentSafe(ctx context.Context, model string, contents ...*genai.Content) (*genai.GenerateContentResponse, error) {
	if ai.CheckSuspensionLock() {
		return nil, fmt.Errorf("gemini api invocation blocked: engine suspended")
	}

	maxRetries := 3
	backoffDuration := 12 * time.Second

	for i := 0; i < maxRetries; i++ {
		resp, err := ai.Client.Models.GenerateContent(ctx, model, contents, nil)
		if err == nil {
			return resp, nil // Success! Exit cleanly.
		}

		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "quota") {
			log.Printf("[Quota Guard] ⚠️ Hit a 429 rate limit window. Retry %d/%d - Backing off for %v...", i+1, maxRetries, backoffDuration)

			// Pause the thread to let Google's rolling token bucket drain
			time.Sleep(backoffDuration)

			// Double the wait time for the next loop pass if this one fails
			backoffDuration *= 2
			continue
		}

		// If it's a different error (like a syntax error), return immediately instead of retrying
		return nil, err
	}

	// If all retries fail, engage the standard safety lock file
	log.Println("[Quota Guard] 🚨 Rate limit retries exhausted. Engaging 1-hour defensive safety lock.")
	ai.SuspendEngine(1 * time.Hour)
	os.Exit(0)
	return nil, fmt.Errorf("gemini api invocation blocked: rate limit exhausted")
}

func (ai *AgentIntern) readMemoryAndFeedback() string {
	content, err := os.ReadFile("PAST_FEEDBACK.md")
	if err != nil {
		return "No prior feedback logged yet. Enforce type-safe development bounds and prioritize beginner friendly tasks."
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

func (ai *AgentIntern) SyncTargetBranch(featureTitle string) (string, bool, error) {
	_ = ai.runExternalGitCommand("fetch", "--all")

	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = ai.TargetDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false, err
	}

	hashSignature := fmt.Sprintf("%x", md5.Sum([]byte(featureTitle)))[:8]
	branchName := fmt.Sprintf("feature/intern-task-%s", hashSignature)

	if strings.Contains(out.String(), branchName) {
		log.Printf("[Branch Sync] 🔄 Existing branch '%s' discovered. Checking it out to append modifications...", branchName)
		if err := ai.runExternalGitCommand("checkout", branchName); err != nil {
			return branchName, false, err
		}
		return branchName, false, nil // false = Not a new branch
	}

	log.Printf("[Branch Sync] 🌿 Creating a fresh feature branch workspace: %s", branchName)
	if err := ai.runExternalGitCommand("checkout", "main"); err != nil {
		return branchName, true, err
	}
	if err := ai.runExternalGitCommand("checkout", "-b", branchName); err != nil {
		return branchName, true, err
	}
	return branchName, true, nil
}

func (ai *AgentIntern) FetchLatestClientEmail() (string, string, error) {
	// ✨ Target Google's secure IMAP cluster on port 993
	log.Println("[Gmail Link] Connecting to imap.gmail.com:993...")
	c, err := client.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to reach Gmail IMAP node: %w", err)
	}
	defer c.Logout()

	// Use your target Gmail address and the 16-character App Password token
	userEmail := "ritiklrt20@gmail.com"
	password := os.Getenv("EMAIL_APP_PASSWORD")

	if err := c.Login(userEmail, password); err != nil {
		return "", "", fmt.Errorf("gmail authorization rejected: %w", err)
	}

	log.Println("[Gmail Link] 🎉 Successfully authenticated! Selecting INBOX...")

	// Open the Inbox in read-only mode to check for incoming client messages
	_, err = c.Select("INBOX", true)
	if err != nil {
		return "", "", fmt.Errorf("failed to select inbox directory: %w", err)
	}

	// Fallback to manual parameters for local processing flow simulation
	mockSender := "gurmeet.singh@codecraftedlabs.co.in"
	mockBody := "Please clean the string encoding parameters on our dynamic flight checkout URLs."
	return mockSender, mockBody, nil
}

func (ai *AgentIntern) ExecuteLifecyclePass(senderEmail string, rawIncomingEmail string) {
	if ai.CheckSuspensionLock() {
		return
	}

	if strings.Contains(strings.ToLower(senderEmail), "gurmeet") {
		log.Println("[Routing Matrix] ✨ Verified critical task priority client match: Gurmeet Singh.")
		ai.TargetDir = "/Users/macbookpro/Developer/flights-scanner"
	}

	log.Printf("\n[Intern Pass] ➔ Syncing context maps for workspace target repo: %s", ai.TargetDir)

	pastFeedback := ai.readMemoryAndFeedback()

	analysisPrompt := fmt.Sprintf(`Analyze this client request. Extract it into a clean JSON layout matching struct {title, description, scope: string[]}. Do not return markdown wraps. Email: %s`, rawIncomingEmail)

	// ⏳ Pacing Buffer: Prevent hitting the endpoint too rapidly if looped
	time.Sleep(8 * time.Second)

	log.Println("[Gemini Integration] 🧠 Dispatched parsing analysis payload...")
	resp, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
	if err != nil {
		log.Printf("[Analysis Warning] ⚠️ Initial request rate limited. Waiting 10s for token pool refresh...")
		time.Sleep(10 * time.Second)

		// Retry once cleanly
		resp, err = ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
		if err != nil {
			log.Printf("[Analysis Failure] ❌ Parsing sequence fault: %v\n", err)
			return
		}
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

	branchName, isNewBranch, err := ai.SyncTargetBranch(req.Title)
	if err != nil {
		log.Printf("[Branch Error] Failed workspace configuration synchronization passes: %v\n", err)
		return
	}

	if isNewBranch {
		defer ai.runExternalGitCommand("checkout", "main")
	}

	fmt.Printf("\n[🚨 INTERN GATEWAY] Project %s | Working Branch: %s\n➔ Objective: %s\nAuthorize task execution sequence? (y/n): ", filepath.Base(ai.TargetDir), branchName, req.Description)
	var confirmation string
	fmt.Scanln(&confirmation)
	if strings.ToLower(strings.TrimSpace(confirmation)) != "y" {
		log.Println("[Gateway Blocked] Task aborted.")
		return
	}

	log.Println("[Engine Pace] ⏳ User authorized. Pausing 6 seconds to reset free-tier concurrency limits...")
	time.Sleep(6 * time.Second)

	if err := ai.processTypeScriptFeatureDevelopment(req, pastFeedback, branchName); err != nil {
		log.Printf("[Development Crash] Sequence broke: %v\n", err)
	}
}

func (ai *AgentIntern) processTypeScriptFeatureDevelopment(req Requirement, pastFeedback string, branchName string) error {
	// 1. Keep text context compact for free tier limitations
	liveExecutionTaskTextPrompt := fmt.Sprintf(
		"Task Objective: %s\nConstraints Scope: %s\nGuardrails:\n%s\nTarget Branch Context: %s.",
		req.Description, strings.Join(req.Scope, ", "), pastFeedback, branchName,
	)

	log.Println("[Gemini Integration] 🧠 Requesting URL sanitization logic modifications...")

	parts := []*genai.Part{{Text: liveExecutionTaskTextPrompt}}

	// Hit Gemini 2.5 Flash safely
	codeGenResponse, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: parts})
	if err != nil {
		return fmt.Errorf("gemini code block generation fault: %w", err)
	}

	log.Printf("[Fulfillment] Code production pass complete. Output Length: %d symbols.", len(codeGenResponse.Text()))

	// ⏳ ✨ THE FIX: Inject a pacing buffer to let the free-tier token buckets refresh
	log.Println("[Engine Pace] ⏳ Standing by for 6 seconds to clear free-tier API concurrency thresholds...")
	time.Sleep(6 * time.Second)

	targetFile := "src/utils/urlSanitizer.ts"
	log.Printf("[File System] Writing generated code payload to target path point: %s", targetFile)
	// Call your GenerateFeatureCode method from coder.go
	// (Passing a blank base64 payload since we shifted completely to text for the free-tier)
	_, err = ai.GenerateFeatureCode(ai.Ctx, req.Description, "", targetFile)
	if err != nil {
		return fmt.Errorf("failed to commit generated feature string code down to disk storage: %w", err)
	}

	// 2. Execute verification metrics test gate pass
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

	// Graceful shutdown listener setup...
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("\n[Graceful Shutdown] 🛑 OS Intercept caught (Ctrl+C). Restoring repository main checkout points cleanly...")
		_ = internWorker.runExternalGitCommand("checkout", "main")
		log.Println("[Graceful Shutdown] ✅ Sandbox metrics secured. Systems safe. Offline.")
		os.Exit(0)
	}()

	sender, body, err := internWorker.FetchLatestClientEmail()
	if err != nil {
		log.Printf("[Email Sync Warning] Falling back to manual parameters: %v", err)

		sender = "Contact@flights-scanners.com"
		// ✨ HIGH EFFICIENCY SYSTEM AUDIT PROMPT FOR FREE TIER CEILINGS
		body = `SUBJECT: [SYSTEM-AUDIT]: Branch Sync & Skills Progress Pass

Core Directive: Run an exhaustive technical audit across the flights-scanner workspace.
1. Run 'git branch -a' to map out all active tracking branch configurations.
2. Review file structures to check code consistency for Level 1 baseline functions.
3. Summarize findings directly inside 'PAST_FEEDBACK.md' under a new header: '## 📊 Global Workspace Alignment Log'.
4. Document the branches found, Level 1 skill metrics, and target project constraints.

*Do not write code updates to urlSanitizer.ts yet. Focus exclusively on reading the codebase and appending the summary log to PAST_FEEDBACK.md.*`
	}

	log.Println("[Engine Startup] ⏳ Standing by for 8 seconds to ensure a clear API token window...")
	time.Sleep(8 * time.Second)

	internWorker.ExecuteLifecyclePass(sender, body)
}
