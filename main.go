package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

// Operational Mode Configuration
const useLocalLLM = true // Set to false to switch back to Gemini Cloud

type Requirement struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Scope       []string `json:"scope"`
}

type AgentIntern struct {
	TargetDir string
	Ctx       context.Context
	Client    *genai.Client
}

func NewAgentIntern(targetDir string) *AgentIntern {
	ctx := context.Background()
	// Initialize cloud client layout cleanly
	client, _ := genai.NewClient(ctx, nil)

	return &AgentIntern{
		TargetDir: targetDir,
		Ctx:       ctx,
		Client:    client,
	}
}

// ============================================================================
// 💻 LOCAL WORKSPACE RUNTIME ENGINE (OLLAMA VIA EMULATED OPENAI STRUCTURES)
// ============================================================================

type OllamaRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type OllamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaResponse struct {
	Message OllamaMessage `json:"message"`
}

func (ai *AgentIntern) GenerateContentLocal(ctx context.Context, promptText string) (string, error) {
	log.Println("[Local LLM] 💻 Dispatching structural matrix to Ollama (qwen2.5-coder)...")
	url := "http://localhost:11434/api/chat"

	reqBody := OllamaRequest{
		Model: "qwen2.5-coder:7b",
		Messages: []OllamaMessage{
			{Role: "user", Content: promptText},
		},
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to encode local configuration: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to initialize network block: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama engine unreachable. Make sure 'ollama serve' is running: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response bytes: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("local node failure code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var ollamaResp OllamaResponse
	if err := json.Unmarshal(bodyBytes, &ollamaResp); err != nil {
		return "", fmt.Errorf("failed to parse structured response matrix: %w", err)
	}

	return ollamaResp.Message.Content, nil
}

// ============================================================================
// ☁️ CLOUD LOGIC WITH RESILIENT EXPONENTIAL BACKOFF RETRY
// ============================================================================

func (ai *AgentIntern) GenerateContentSafe(ctx context.Context, model string, contents ...*genai.Content) (*genai.GenerateContentResponse, error) {
	if ai.CheckSuspensionLock() {
		return nil, fmt.Errorf("gemini api invocation blocked: engine suspended")
	}

	maxRetries := 3
	backoffDuration := 12 * time.Second

	for i := 0; i < maxRetries; i++ {
		resp, err := ai.Client.Models.GenerateContent(ctx, model, contents, nil)
		if err == nil {
			return resp, nil
		}

		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "quota") {
			log.Printf("[Quota Guard] ⚠️ Hit 429 rate limit window. Retry %d/%d - Cooling down for %v...", i+1, maxRetries, backoffDuration)
			time.Sleep(backoffDuration)
			backoffDuration *= 2
			continue
		}
		return nil, err
	}

	log.Println("[Quota Guard] 🚨 Rate limit retries exhausted. Engaging 1-hour defensive safety lock.")
	ai.SuspendEngine(1 * time.Hour)
	os.Exit(0)
	return nil, fmt.Errorf("gemini api invocation blocked: rate limit exhausted")
}

// ============================================================================
// 🔐 INBOX SYNC & REPOSITORY INTEGRITY AGENT
// ============================================================================

func (ai *AgentIntern) FetchLatestClientEmail() (string, string, error) {
	log.Println("[Gmail Link] Connecting to imap.gmail.com:993...")
	c, err := client.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to reach Gmail IMAP node: %w", err)
	}
	defer c.Logout()

	userEmail := "ritiklrt20@gmail.com"
	password := os.Getenv("EMAIL_APP_PASSWORD")

	if err := c.Login(userEmail, password); err != nil {
		return "", "", fmt.Errorf("gmail authorization rejected: %w", err)
	}

	log.Println("[Gmail Link] 🎉 Successfully authenticated! Selecting INBOX...")
	_, err = c.Select("INBOX", true)
	if err != nil {
		return "", "", fmt.Errorf("failed to select inbox directory: %w", err)
	}

	// Dynamic fallback parameter loading simulation
	mockSender := "Contact@flights-scanners.com"
	mockBody := `SUBJECT: [SYSTEM-AUDIT]: Branch Sync & Skills Progress Pass

Core Directive: Run an exhaustive technical audit across the flights-scanner workspace.
1. Run 'git branch -a' to map out all active tracking branch configurations.
2. Review file structures to check code consistency for Level 1 baseline functions.
3. Summarize findings directly inside 'PAST_FEEDBACK.md' under a new header: '## 📊 Global Workspace Alignment Log'.
4. Document the branches found, Level 1 skill metrics, and target project constraints.`

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

	var rawResponseText string
	analysisPrompt := fmt.Sprintf(`Analyze this engineering payload. Extract the text target into a strict JSON scheme matching struct {title, description, scope: string[]}. Do not wrap inside markdown ticks. Content: %s`, rawIncomingEmail)

	// Route to Local Macbook Hardware vs Google Cloud
	if useLocalLLM {
		var err error
		rawResponseText, err = ai.GenerateContentLocal(ai.Ctx, analysisPrompt)
		if err != nil {
			log.Printf("[Local Fallback Failure] Switching back to safe cloud layers: %v", err)
			useCloudFallback := true
			if useCloudFallback {
				resp, _ := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
				rawResponseText = resp.Text()
			}
		}
	} else {
		resp, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
		if err != nil {
			log.Printf("[Analysis Failure] ❌ Parsing sequence fault: %v\n", err)
			return
		}
		rawResponseText = resp.Text()
	}

	var req Requirement
	cleanJSON := strings.TrimPrefix(strings.TrimSuffix(rawResponseText, "```"), "```json")
	cleanJSON = strings.TrimSpace(cleanJSON)

	if err := json.Unmarshal([]byte(cleanJSON), &req); err != nil {
		req = Requirement{
			Title:       "System Architectural Audit Pass",
			Description: rawIncomingEmail,
			Scope:       []string{"Audit repository branch matrix trees", "Document parameters inside PAST_FEEDBACK.md"},
		}
	}

	branchName, isNewBranch, err := ai.SyncTargetBranch(req.Title)
	if err != nil {
		log.Printf("[Branch Error] Failed workspace configuration steps: %v\n", err)
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

	// ⏳ Defensive free tier buffer to let sockets settle between cycles
	log.Println("[Engine Pace] ⏳ User authorized. Preparing target execution matrices...")
	time.Sleep(2 * time.Second)

	if err := ai.processTypeScriptFeatureDevelopment(req, pastFeedback, branchName); err != nil {
		log.Printf("[Development Crash] Sequence broke: %v\n", err)
	}
}

func (ai *AgentIntern) processTypeScriptFeatureDevelopment(req Requirement, pastFeedback string, branchName string) error {
	log.Println("[Gemini Integration] 🧠 Formulating engineering verification context updates...")

	runLogPrompt := fmt.Sprintf(`You are an automated software engineer loop acting as an intern. Review this task directive: %s. Update the 'PAST_FEEDBACK.md' log parameters under a new section titled '## 📊 Global Workspace Alignment Log'. Write down the historical branch contexts found, Level 1 competency profiles, and client safety rules for Gurmeet Singh. Historical context file data: %s`, req.Description, pastFeedback)

	var logUpdates string
	if useLocalLLM {
		var err error
		logUpdates, err = ai.GenerateContentLocal(ai.Ctx, runLogPrompt)
		if err != nil {
			return err
		}
	} else {
		resp, err := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: runLogPrompt}}})
		if err != nil {
			return err
		}
		logUpdates = resp.Text()
	}

	// Append findings safely to the workspace tracking memory matrix
	feedbackPath := filepath.Join(ai.TargetDir, "PAST_FEEDBACK.md")
	f, err := os.OpenFile(feedbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to mount memory tracking file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + logUpdates + "\n"); err != nil {
		return fmt.Errorf("failed to commit structural stream changes: %w", err)
	}

	log.Println("[Fulfillment] ✅ Structural inspection update committed directly to PAST_FEEDBACK.md successfully.")
	return nil
}

// ============================================================================
// 🛠️ GIT ENVIRONMENT UTILITIES & LOCAL FILE MEMORY CONTROLLERS
// ============================================================================

func (ai *AgentIntern) SyncTargetBranch(title string) (string, bool, error) {
	cleanTitle := strings.ToLower(title)
	cleanTitle = strings.ReplaceAll(cleanTitle, " ", "-")
	cleanTitle = strings.ReplaceAll(cleanTitle, ":", "")

	// Create an isolated task identifier hash loop
	taskHash := fmt.Sprintf("%x", time.Now().UnixNano())[:8]
	branchName := fmt.Sprintf("feature/intern-task-%s", taskHash)

	log.Printf("[Branch Sync] 🌿 Creating a fresh feature branch workspace: %s", branchName)

	if err := ai.runExternalGitCommand("checkout", "-b", branchName); err != nil {
		return "", false, err
	}
	return branchName, true, nil
}

func (ai *AgentIntern) runExternalGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = ai.TargetDir
	return cmd.Run()
}

func (ai *AgentIntern) readMemoryAndFeedback() string {
	path := filepath.Join(ai.TargetDir, "PAST_FEEDBACK.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "No existing structural feedback discovered inside repo root layout."
	}
	return string(data)
}

func (ai *AgentIntern) CheckSuspensionLock() bool {
	if _, err := os.Stat("SUSPENDED.lock"); err == nil {
		log.Println("[Quota Guard] 🔒 System execution suspended via active security lock file.")
		return true
	}
	return false
}

func (ai *AgentIntern) SuspendEngine(d time.Duration) {
	_ = os.WriteFile("SUSPENDED.lock", []byte(time.Now().Add(d).Format(time.RFC3339)), 0644)
}

// ============================================================================
// 🏃‍♂️ AGENT INVOCATION ENGINE
// ============================================================================

func main() {
	defaultTargetRepo := "/Users/macbookpro/Developer/flights-scanner"
	internWorker := NewAgentIntern(defaultTargetRepo)

	// Clean Graceful Shutdown Intercept Strategy
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("\n[Graceful Shutdown] 🛑 OS Intercept caught (Ctrl+C). Restoring repository checkout targets...")
		_ = internWorker.runExternalGitCommand("checkout", "main")
		log.Println("[Graceful Shutdown] ✅ Repository parameters restored cleanly. Offline.")
		os.Exit(0)
	}()

	sender, body, err := internWorker.FetchLatestClientEmail()
	if err != nil {
		log.Printf("[Email Sync Warning] Falling back to manual parameters: %v", err)
		sender = "Contact@flights-scanners.com"
	}

	log.Println("[Engine Startup] ⏳ Standing by for 3 seconds to clear execution loops safely...")
	time.Sleep(3 * time.Second)

	internWorker.ExecuteLifecyclePass(sender, body)
}
