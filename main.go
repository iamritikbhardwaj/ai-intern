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

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

const useLocalLLM = true

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
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Printf("[Warning] Cloud Gemini initialization bypassed: %v", err)
	}

	return &AgentIntern{
		TargetDir: targetDir,
		Ctx:       ctx,
		Client:    client,
	}
}

// ============================================================================
// 💻 LOCAL WORKSPACE RUNTIME ENGINE (OLLAMA)
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

// InspectExistingFeatures searches the local workspace for files matching the requirement target keys
func (ai *AgentIntern) InspectExistingFeatures(scope []string) string {
	if len(scope) == 0 {
		return ""
	}

	log.Println("[Codebase Scan] 🔍 Checking for pre-existing features matching scope keywords...")
	var scanResults []string

	// Look for source files where these keywords might already exist
	extensions := []string{".ts", ".tsx", ".js", ".json"}

	err := filepath.Walk(ai.TargetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip heavy directories like node_modules or .git
		if strings.Contains(path, "node_modules") || strings.Contains(path, ".git") || strings.Contains(path, "dist") {
			return filepath.SkipDir
		}

		// Only scan relevant code extensions
		validExt := false
		for _, ext := range extensions {
			if filepath.Ext(path) == ext {
				validExt = true
				break
			}
		}
		if !validExt {
			return nil
		}

		// Read file contents to look for feature overlaps
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(contentBytes)

		for _, keyword := range scope {
			// Clean up the keyword to isolate core text components
			cleanKey := strings.TrimSpace(strings.ToLower(keyword))
			if len(cleanKey) < 3 {
				continue
			}

			// If the code contains a match, document its location for the model context
			if strings.Contains(strings.ToLower(content), cleanKey) {
				relPath, _ := filepath.Rel(ai.TargetDir, path)
				scanResults = append(scanResults, fmt.Sprintf("- Found matching keyword '%s' inside: %s", keyword, relPath))
				break
			}
		}
		return nil
	})

	if err != nil || len(scanResults) == 0 {
		return "No conflicting pre-existing code structures discovered."
	}

	return strings.Join(scanResults, "\n")
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
// 🔐 INBOX LIVE FETCH LAYER (NO MOCK FALLBACKS)
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

	_, err = c.Select("INBOX", false)
	if err != nil {
		return "", "", fmt.Errorf("failed to select inbox directory: %w", err)
	}

	// Look strictly for UNSEEN messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		return "", "", fmt.Errorf("failed to search unseen messages: %w", err)
	}

	if len(ids) == 0 {
		return "", "", nil // Clean empty signal
	}

	// Fetch the single latest email metadata block
	seqset := new(imap.SeqSet)
	seqset.AddNum(ids[len(ids)-1])

	var section imap.BodySectionName
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}
	messages := make(chan *imap.Message, 1)

	go func() {
		if err := c.Fetch(seqset, items, messages); err != nil {
			log.Printf("[IMAP Error] Failed to fetch sequence details: %v", err)
		}
	}()

	msg := <-messages
	if msg == nil {
		return "", "", fmt.Errorf("fetched message envelope empty")
	}

	sender := ""
	if len(msg.Envelope.From) > 0 {
		sender = fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
	}

	r := msg.GetBody(&section)
	if r == nil {
		return sender, "Empty Email Body", nil
	}

	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r)

	return sender, buf.String(), nil
}

// ============================================================================
// ⚙️ WORKSPACE LIFECYCLE CONTROLLER WITH INTEGRITY GATES
// ============================================================================

func (ai *AgentIntern) ExecuteLifecyclePass(senderEmail string, rawIncomingEmail string) {
	if ai.CheckSuspensionLock() {
		return
	}

	// 📋 ✨ THE MULTI-SENDER MATRIX: Add any email addresses or names to track here
	prioritySenders := []string{
		"Contact@flights-scanners.com",
		"ritiklrt20@gmail.com",
	}

	isPriorityClient := false
	senderLower := strings.ToLower(senderEmail)

	for _, priority := range prioritySenders {
		if strings.Contains(senderLower, strings.ToLower(priority)) {
			isPriorityClient = true
			log.Printf("[Routing Matrix] ✨ Verified critical task priority client match: %s", senderEmail)
			break
		}
	}

	// Route configuration based on priority evaluation pass
	if isPriorityClient {
		ai.TargetDir = "/Users/macbookpro/Developer/flights-scanner"
	} else {
		// Optional: Handle non-priority paths or use a default path configuration safely
		log.Printf("[Routing Matrix] ⏳ Routine email ingestion pass for sender: %s", senderEmail)
	}

	log.Printf("\n[Intern Pass] ➔ Syncing context maps for workspace target repo: %s", ai.TargetDir)
	pastFeedback := ai.readMemoryAndFeedback()

	var rawResponseText string
	analysisPrompt := fmt.Sprintf(`Analyze this engineering payload. Extract the text target into a strict JSON scheme matching struct {title, description, scope: string[]}. Do not wrap inside markdown ticks. Content: %s`, rawIncomingEmail)

	if useLocalLLM {
		var err error
		rawResponseText, err = ai.GenerateContentLocal(ai.Ctx, analysisPrompt)
		if err != nil {
			log.Printf("[Local Fallback Failure] Switching to cloud: %v", err)
			resp, _ := ai.GenerateContentSafe(ai.Ctx, "gemini-2.5-flash", &genai.Content{Parts: []*genai.Part{{Text: analysisPrompt}}})
			rawResponseText = resp.Text()
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
			Scope:       []string{"Audit repository branch matrix trees"},
		}
	}

	existingCodeContext := ai.InspectExistingFeatures(req.Scope)
	if existingCodeContext != "No conflicting pre-existing code structures discovered." {
		log.Println("[Codebase Scan] ⚠️ Warning: Potential feature overlap detected inside the workspace.")
	}

	// Check if this directive involves external API integration
	needsExternalDocs := false
	combinedContent := strings.ToLower(req.Description + " " + strings.Join(req.Scope, " "))
	if strings.Contains(combinedContent, "api") || strings.Contains(combinedContent, "integrate") || strings.Contains(combinedContent, "external") {
		needsExternalDocs = true
	}

	var docPayload string
	if needsExternalDocs {
		docPath := filepath.Join(ai.TargetDir, "docs", "new_api_specs.md")
		data, err := os.ReadFile(docPath)
		if err != nil {
			log.Printf("\n[🚨 HARD HALT] Task requires external API integration, but no documentation was found at 'docs/new_api_specs.md'. Stopping task sequence to prevent code hallucination.")
			return
		}
		docPayload = string(data)
		log.Println("[Workspace Gate] ✅ Verified external API specification reference layout.")
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

	log.Println("[Engine Pace] ⏳ User authorized. Preparing target execution matrices...")
	time.Sleep(2 * time.Second)

	if err := ai.processTypeScriptFeatureDevelopment(req, pastFeedback, branchName, docPayload); err != nil {
		log.Printf("[Development Crash] Sequence broke: %v\n", err)
	}
}

func (ai *AgentIntern) processTypeScriptFeatureDevelopment(req Requirement, pastFeedback string, branchName string, docPayload string) error {
	log.Println("[Workspace Inspection] 📂 Querying active repository branch tree...")

	var realBranches string
	cmdBranches := exec.Command("git", "branch", "-a")
	cmdBranches.Dir = ai.TargetDir
	if out, err := cmdBranches.Output(); err == nil {
		realBranches = string(out)
	}

	var realFiles string
	cmdFiles := exec.Command("git", "ls-files")
	cmdFiles.Dir = ai.TargetDir
	if out, err := cmdFiles.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 15 {
			realFiles = strings.Join(lines[:15], "\n") + "\n... (truncated)"
		} else {
			realFiles = string(out)
		}
	}

	log.Println("[Local LLM] 🧠 Formulating factual engineering verification log...")

	runLogPrompt := fmt.Sprintf(`You are an automated software engineer loop tracking workspace progress.
	
	CRITICAL PROTOCOL:
	1. Review the "Prior Feedback Context" section below carefully. Look for past mistakes, rules, or user constraints documented in previous runs.
	2. Analyze the current environment state (Branches and Files).
	3. Generate a new "## 📊 Global Workspace Alignment Log" section. Do NOT copy paste old logs. Write a fresh update documenting the factual state of this current run, ensuring you do not repeat errors noted in the history.

	--- ENVIRONMENT CONTEXT ---
	Target Repository: %s
	Current Branch Context: %s
	
	Discovered System Branch List:
	%s

	Discovered Workspace Files (Top 15):
	%s

	External API Documentation Specification (If Applicable):
	%s

	Prior Feedback Context (Your Historical Memory):
	%s
	`, ai.TargetDir, branchName, realBranches, realFiles, docPayload, pastFeedback)

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

	feedbackPath := filepath.Join(ai.TargetDir, "PAST_FEEDBACK.md")
	f, err := os.OpenFile(feedbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open feedback file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + logUpdates + "\n"); err != nil {
		return fmt.Errorf("failed to commit structural logs: %w", err)
	}

	log.Println("[Fulfillment] ✅ Factual audit log committed to PAST_FEEDBACK.md.")
	return nil
}

// ============================================================================
// 🛠️ ENVIRONMENT & FILE UTILITIES
// ============================================================================

func (ai *AgentIntern) SyncTargetBranch(title string) (string, bool, error) {
	cleanTitle := strings.ToLower(title)
	cleanTitle = strings.ReplaceAll(cleanTitle, " ", "-")
	cleanTitle = strings.ReplaceAll(cleanTitle, ":", "")
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

func validateEnvironment() {
	_ = godotenv.Load() // Load .env file if it exists, otherwise rely on system env
	requiredVars := []string{
		"GEMINI_API_KEY",
		"EMAIL_APP_PASSWORD",
	}
	var missingVars []string
	for _, reqVar := range requiredVars {
		if os.Getenv(reqVar) == "" {
			missingVars = append(missingVars, reqVar)
		}
	}
	if len(missingVars) > 0 {
		log.Fatalf("🚨 Environment Validation Failed: Missing required configuration keys: [%s]", strings.Join(missingVars, ", "))
	}
}

func main() {
	validateEnvironment()
	defaultTargetRepo := "/Users/macbookpro/Developer/flights-scanner"
	internWorker := NewAgentIntern(defaultTargetRepo)

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
		log.Printf("[Email Link Failure] %v", err)
		os.Exit(1)
	}

	if sender == "" && body == "" {
		log.Println("[Inbox Monitor] 🔍 No unread workflow requirements found. Exiting.")
		os.Exit(0)
	}

	// 📋 ✨ UPDATED MULTI-SENDER MATRIX FRONT GATE
	prioritySenders := []string{
		"singh98091@gmail.com,",
		"Contact@flights-scanners.com",
		"ritiklrt2@gmail.com",
		"ritik@codecraftedlabs.co.in",
	}

	matchedPriority := false
	senderLower := strings.ToLower(sender)

	for _, priority := range prioritySenders {
		if strings.Contains(senderLower, strings.ToLower(priority)) {
			matchedPriority = true
			break
		}
	}

	if !matchedPriority {
		log.Printf("[Inbox Monitor] ⏳ Found unread email from non-priority sender (%s). Skipping execution pass.", sender)
		os.Exit(0)
	}

	log.Println("[Engine Startup] ⏳ Standing by for 3 seconds to clear execution loops safely...")
	time.Sleep(3 * time.Second)

	internWorker.ExecuteLifecyclePass(sender, body)
}
