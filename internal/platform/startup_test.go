package platform

import "testing"

func TestStartupCommandQuotesExecutablePath(t *testing.T) {
	got := QuoteStartupCommand(`C:\Users\Test User\AppData\Local\Programs\LumiDesk\LumiDesk.exe`)
	want := `"C:\Users\Test User\AppData\Local\Programs\LumiDesk\LumiDesk.exe" --startup`
	if got != want {
		t.Fatalf("startup command = %q, want %q", got, want)
	}
}
