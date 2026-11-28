//go:build !windows

package main

import "github.com/zdraganov/Score52/internal/web"

// BridgeMate needs Windows (the Access driver and BCS).
func bridgemateHost(template, bcs string) web.BwsHost { return nil }
