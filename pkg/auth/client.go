package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// AuthClient handles unified browser-based authentication.
type AuthClient struct {
	apiBaseURL string
}

// callbackResult holds the result of the OAuth callback.
type callbackResult struct {
	tokens *TokenResponse
	err    error
}

// NewAuthClient creates a new authentication client.
func NewAuthClient(apiBaseURL string) *AuthClient {
	return &AuthClient{
		apiBaseURL: apiBaseURL,
	}
}

// Login opens the browser to the API's login page and waits for the callback.
func (c *AuthClient) Login(ctx context.Context) (*TokenResponse, error) {
	// Start a local HTTP server on a random available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local server: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	slog.Debug("Started local callback server", "port", port)

	// Generate random state for CSRF protection
	state, err := generateRandomState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Build the login URL that points to the API's login page
	// The API will show both Google and userpass options
	// Pass redirect_uri so the API knows where to send the user after authentication
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)
	loginURL, err := loginURL(c.apiBaseURL, redirectURI, state)
	if err != nil {
		return nil, err
	}

	// Create a channel to receive the callback result
	resultChan := make(chan callbackResult, 1)

	// Set up the HTTP server with callback handler
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		c.handleCallback(w, r, state, resultChan)
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start the server in a goroutine
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "err", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown server", "err", err)
		}
	}()

	// Open browser to the login page
	if err := openBrowser(loginURL); err != nil {
		fmt.Printf("Failed to open browser automatically. Please visit:\n%s\n", loginURL)
	}

	// Wait for callback or timeout
	select {
	case result := <-resultChan:
		if result.err != nil {
			return nil, result.err
		}
		return result.tokens, nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 5 minutes")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func loginURL(apiBaseURL, redirectURI, state string) (string, error) {
	base, err := url.Parse(apiBaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid API base URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("API base URL must use http or https")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/login"
	values := base.Query()
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state)
	base.RawQuery = values.Encode()
	return base.String(), nil
}

// handleCallback processes the callback from the API after authentication.
func (c *AuthClient) handleCallback(w http.ResponseWriter, r *http.Request, expectedState string, resultChan chan<- callbackResult) {
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")
	errorDesc := r.URL.Query().Get("error_description")

	if errorParam != "" {
		resultChan <- callbackResult{
			err: fmt.Errorf("authentication error: %s - %s", errorParam, errorDesc),
		}
		http.Error(w, fmt.Sprintf("Authentication failed: %s", errorDesc), http.StatusBadRequest)
		return
	}

	if state != expectedState {
		resultChan <- callbackResult{
			err: fmt.Errorf("invalid state parameter"),
		}
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	accessToken, idToken, tokenType, expiresIn, err := extractCallbackTokens(r)
	if err != nil {
		resultChan <- callbackResult{err: err}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if idToken == "" && r.Method == http.MethodGet {
		writeTokenRelayPage(w)
		return
	}

	if idToken == "" {
		resultChan <- callbackResult{
			err: fmt.Errorf("authentication completed but no tokens received"),
		}
		http.Error(w, "No tokens received", http.StatusBadRequest)
		return
	}

	// Calculate expiry date
	expiryDate := time.Now().Unix() + int64(expiresIn)

	tokens := &TokenResponse{
		AccessToken: accessToken,
		IDToken:     idToken,
		TokenType:   tokenType,
		ExpiryDate:  expiryDate,
		Scope:       "openid email profile",
	}

	writeSuccessPage(w)
	resultChan <- callbackResult{tokens: tokens}
}

func extractCallbackTokens(r *http.Request) (string, string, string, int, error) {
	var accessToken, idToken string
	tokenType := "Bearer"
	expiresIn := 3600

	setExpiresIn := func(raw string) {
		if raw == "" {
			return
		}
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			expiresIn = parsed
		}
	}

	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case "vault_token":
			accessToken = cookie.Value
			if cookie.MaxAge > 0 {
				expiresIn = cookie.MaxAge
			}
		case "id_token":
			idToken = cookie.Value
		case "session":
			if accessToken == "" {
				accessToken = cookie.Value
			}
			if idToken == "" {
				idToken = cookie.Value
			}
			if cookie.MaxAge > 0 {
				expiresIn = cookie.MaxAge
			}
		}
	}

	values := r.URL.Query()
	if idToken == "" {
		idToken = values.Get("id_token")
	}
	if accessToken == "" {
		accessToken = values.Get("access_token")
	}
	if values.Get("token_type") != "" {
		tokenType = values.Get("token_type")
	}
	setExpiresIn(values.Get("expires_in"))

	if r.Method == http.MethodPost {
		payload, err := parseCallbackPayload(r)
		if err != nil {
			return "", "", "", 0, err
		}
		if payload["error"] != "" {
			return "", "", "", 0, fmt.Errorf("authentication failed: %s", payload["error"])
		}
		if idToken == "" {
			idToken = payload["id_token"]
		}
		if accessToken == "" {
			accessToken = payload["access_token"]
		}
		if payload["token_type"] != "" {
			tokenType = payload["token_type"]
		}
		setExpiresIn(payload["expires_in"])
	}

	if accessToken == "" {
		accessToken = idToken
	}
	if idToken == "" {
		idToken = accessToken
	}
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return accessToken, idToken, tokenType, expiresIn, nil
}

func parseCallbackPayload(r *http.Request) (map[string]string, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, fmt.Errorf("invalid callback payload: %w", err)
		}
		return payload, nil
	}

	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("invalid callback form: %w", err)
	}
	payload := make(map[string]string, len(r.PostForm))
	for key := range r.PostForm {
		payload[key] = r.PostForm.Get(key)
	}
	return payload, nil
}

func writeTokenRelayPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	relayHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>Completing Authentication</title>
</head>
<body>
    <p id="message">Completing authentication...</p>
    <script>
        const params = new URLSearchParams(window.location.hash.replace(/^#/, ""));
        const payload = Object.fromEntries(params.entries());
        if (!payload.id_token && !payload.access_token) {
            payload.error = "missing tokens in callback";
        }
        fetch(window.location.pathname + window.location.search, {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify(payload)
        }).then(function(response) {
            if (!response.ok) {
                document.getElementById("message").textContent = "Authentication failed. Return to your terminal for details.";
                return;
            }
            return response.text().then(function(html) {
                document.open();
                document.write(html);
                document.close();
            });
        }).catch(function() {
            document.getElementById("message").textContent = "Authentication failed. Return to your terminal for details.";
        });
    </script>
</body>
</html>
`
	if _, err := w.Write([]byte(relayHTML)); err != nil {
		slog.Error("Failed to write response", "err", err)
	}
}

func writeSuccessPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	successHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>Authentication Successful</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background-color: #f5f5f5;
        }
        .container {
            text-align: center;
            background: white;
            padding: 40px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            margin-bottom: 10px;
        }
        p {
            color: #666;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Authentication Successful!</h1>
        <p>You can close this window and return to your terminal.</p>
    </div>
    <script>
        setTimeout(function() {
            window.close();
        }, 2000);
    </script>
</body>
</html>
`
	if _, err := w.Write([]byte(successHTML)); err != nil {
		slog.Error("Failed to write response", "err", err)
	}
}

// generateRandomState generates a cryptographically secure random state parameter.
func generateRandomState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:32], nil
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204 -- fixed browser opener command; URL is passed as an argument, not shell-evaluated.
	case "linux":
		cmd = exec.Command("xdg-open", url) // #nosec G204 -- fixed browser opener command; URL is passed as an argument, not shell-evaluated.
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url) // #nosec G204 -- fixed shell builtin invocation needed for Windows default browser opening.
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
