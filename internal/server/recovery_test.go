package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func recoveryCodes(t *testing.T, h *harness, cookie *http.Cookie, password string) []string {
	t.Helper()
	w := h.do(as(post("/cove/recovery-codes", jsonBody(fmt.Sprintf(`{"password":%q}`, password))), cookie))
	var result struct {
		Codes []string `json:"codes"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Codes) != recoveryCodeCount {
		t.Fatalf("generate: %d %s", w.Code, w.Body)
	}
	return result.Codes
}

func TestRecoveryLifecycle(t *testing.T) {
	h, _ := newAccountHarness(t)
	cookie := h.signup("quietfox", "original passphrase")
	other := h.signup("otherfox", "other passphrase")
	if w := h.do(post("/cove/recovery-codes", jsonBody(`{"password":"original passphrase"}`))); w.Code != 401 {
		t.Fatalf("anonymous generation: %d", w.Code)
	}
	if w := h.do(as(post("/cove/recovery-codes", jsonBody(`{"password":"wrong"}`)), cookie)); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	old := recoveryCodes(t, h, cookie, "original passphrase")
	codes := recoveryCodes(t, h, cookie, "original passphrase")
	seen := map[string]bool{}
	for _, code := range codes {
		if len(recoveryHash(code)) != 32 || seen[code] {
			t.Fatal("invalid or duplicate code")
		}
		seen[code] = true
	}
	var stored []byte
	if err := h.srv.accounts.QueryRow("SELECT code_hash FROM account_recovery_codes WHERE code_hash = ?", recoveryHash(codes[0])).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == codes[0] {
		t.Fatal("plaintext code stored")
	}
	me := h.do(as(get("/cove/me"), cookie))
	if !strings.Contains(me.Body.String(), `"recoveryCodesRemaining":10`) || strings.Contains(me.Body.String(), codes[0]) {
		t.Fatalf("me: %s", me.Body)
	}
	recover := func(name, code, next string) int {
		return h.do(post("/cove/recover", jsonBody(fmt.Sprintf(`{"username":%q,"code":%q,"next":%q}`, name, code, next)))).Code
	}
	if status := recover("quietfox", old[0], "new passphrase"); status != 401 {
		t.Fatalf("old set: %d", status)
	}
	if status := recover("otherfox", codes[0], "new passphrase"); status != 401 {
		t.Fatalf("cross-account: %d", status)
	}
	if status := recover("quietfox", codes[0], "short"); status != 400 {
		t.Fatalf("weak password: %d", status)
	}
	if status := recover("QUIETFOX", strings.ToUpper(strings.ReplaceAll(codes[0], "-", "")), "new passphrase"); status != 200 {
		t.Fatalf("reset: %d", status)
	}
	if status := recover("quietfox", codes[0], "another passphrase"); status != 401 {
		t.Fatalf("reuse: %d", status)
	}
	if w := h.do(as(get("/cove/me"), cookie)); w.Code != 401 {
		t.Fatal("old session survived")
	}
	if w := h.do(as(get("/cove/me"), other)); w.Code != 200 {
		t.Fatal("other account session revoked")
	}
	if w := h.do(post("/cove/login", jsonBody(`{"username":"quietfox","password":"original passphrase"}`))); w.Code != 401 {
		t.Fatal("old password survived")
	}
	w := h.do(post("/cove/login", jsonBody(`{"username":"quietfox","password":"new passphrase"}`)))
	cookie = accountCookie(t, w)
	me = h.do(as(get("/cove/me"), cookie))
	if !strings.Contains(me.Body.String(), `"recoveryCodesRemaining":9`) {
		t.Fatalf("remaining: %s", me.Body)
	}
	// Remaining codes stay available for later recovery.
	var count int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM account_recovery_codes").Scan(&count); err != nil || count != 9 {
		t.Fatalf("stored remaining %d: %v", count, err)
	}
	if w := h.do(as(post("/cove/delete", jsonBody(`{"password":"new passphrase"}`)), cookie)); w.Code != 200 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM account_recovery_codes").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade %d: %v", count, err)
	}
}

func TestRecoveryConcurrentRedemption(t *testing.T) {
	h, _ := newAccountHarness(t)
	cookie := h.signup("quietfox", "original passphrase")
	code := recoveryCodes(t, h, cookie, "original passphrase")[0]
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := h.do(post("/cove/recover", jsonBody(fmt.Sprintf(`{"username":"quietfox","code":%q,"next":"new passphrase"}`, code))))
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	successes, denied := 0, 0
	for status := range statuses {
		if status == 200 {
			successes++
		} else if status == 401 {
			denied++
		} else {
			t.Errorf("unexpected status %d", status)
		}
	}
	if successes != 1 || denied != 1 {
		t.Fatalf("successes %d, denied %d", successes, denied)
	}
}

func TestRecoveryRateLimit(t *testing.T) {
	h, _ := newAccountHarness(t)
	for i := 0; i < 30; i++ {
		w := h.do(post("/cove/recover", jsonBody(`{"username":"missing","code":"00000000-00000000-00000000-00000000","next":"new passphrase"}`)))
		if w.Code == 429 {
			return
		}
		if w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	t.Fatal("recovery was not rate limited")
}

func TestRecoveryRollsBackOnFailure(t *testing.T) {
	h, _ := newAccountHarness(t)
	cookie := h.signup("quietfox", "original passphrase")
	code := recoveryCodes(t, h, cookie, "original passphrase")[0]
	if _, err := h.srv.accounts.Exec(`CREATE TRIGGER fail_password_reset BEFORE UPDATE OF password ON accounts BEGIN SELECT RAISE(ABORT, 'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	w := h.do(post("/cove/recover", jsonBody(fmt.Sprintf(`{"username":"quietfox","code":%q,"next":"replacement passphrase"}`, code))))
	if w.Code < 500 {
		t.Fatalf("reset unexpectedly succeeded: %d", w.Code)
	}
	var remaining int
	if err := h.srv.accounts.QueryRow("SELECT count(*) FROM account_recovery_codes WHERE code_hash = ?", recoveryHash(code)).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("code consumed on failure: %d %v", remaining, err)
	}
	if w := h.do(as(get("/cove/me"), cookie)); w.Code != 200 {
		t.Fatal("session revoked on failed recovery")
	}
}
