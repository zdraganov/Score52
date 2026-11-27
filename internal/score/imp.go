package score

import "math"

// impSteps are the lower bounds of the IMP scale: a difference of at least impSteps[i] is worth i+1 IMPs.
var impSteps = [...]int{20, 50, 90, 130, 170, 220, 270, 320, 370, 430, 500, 600, 750, 900, 1100, 1300, 1500, 1750, 2000, 2250, 2500, 3000, 3500, 4000}

// IMP converts a point difference into IMPs, keeping the sign.
func IMP(diff int) int {
	a := diff
	if a < 0 {
		a = -a
	}
	n := 0
	for _, s := range impSteps {
		if a < s {
			break
		}
		n++
	}
	if diff < 0 {
		return -n
	}
	return n
}

// IMPf converts a fractional difference (e.g. against a Butler datum that was not rounded) into IMPs.
func IMPf(diff float64) int { return IMP(int(math.Round(diff))) }
