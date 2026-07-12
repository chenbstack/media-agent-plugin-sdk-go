package pluginsdk

import "testing"

func TestOnboardingAssessmentValidate(t *testing.T) {
	for _, status := range []OnboardingAssessmentStatus{OnboardingNeedsSetup, OnboardingSatisfied} {
		if err := (OnboardingAssessment{Status: status}).Validate(); err != nil {
			t.Fatalf("status %q: %v", status, err)
		}
	}
	if err := (OnboardingAssessment{Status: "hidden"}).Validate(); err == nil {
		t.Fatal("unknown assessment status must fail")
	}
}
