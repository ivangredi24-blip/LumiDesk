//go:build !windows

package main

func runTray(*App) {}
func stopTray()    {}
