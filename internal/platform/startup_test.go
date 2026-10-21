package platform

import "testing"

func TestStartupCommandQuotesExecutablePath(t *testing.T) {
	got := QuoteStartupCommand(`C:\Users\Test User\AppData\Local\Programs\LumiDesk\LumiDesk.exe`)
	want := `"C:\Users\Test User\AppData\Local\Programs\LumiDesk\LumiDesk.exe" --startup`
	if got != want {
		t.Fatalf("startup command = %q, want %q", got, want)
	}
}

func TestBeforeLoginCommandQuotesExecutableAndSettingsPath(t *testing.T) {
	got := QuoteBeforeLoginCommand(`C:\Program Files\LumiDesk\LumiDesk.exe`, `C:\Users\Test User\AppData\Roaming\LumiDesk`)
	want := `"C:\Program Files\LumiDesk\LumiDesk.exe" --prelogin-lights --config-dir "C:\Users\Test User\AppData\Roaming\LumiDesk"`
	if got != want {
		t.Fatalf("pre-login task command = %q, want %q", got, want)
	}
}
