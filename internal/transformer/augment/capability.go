package augment

// ProviderCapabilities captures the subset of upstream features the Augment
// data plane cares about when rendering provider-specific payloads.
type ProviderCapabilities struct {
	SupportsResponsesInput       bool
	SupportsPreviousResponseID   bool
	SupportsStore                bool
	SupportsInstructions         bool
	SupportsToolChoice           bool
	SupportsThinking             bool
	DegradeOrphanToolCallsToText bool
	InjectMissingToolResults     bool
	SupportsContextHistoryRule   bool
}

func capabilitiesForTarget(targetType string) ProviderCapabilities {
	switch targetType {
	case "openai2":
		return ProviderCapabilities{
			SupportsResponsesInput:       true,
			SupportsPreviousResponseID:   false,
			SupportsStore:                false,
			SupportsInstructions:         true,
			SupportsToolChoice:           true,
			SupportsThinking:             true,
			DegradeOrphanToolCallsToText: true,
			InjectMissingToolResults:     true,
			SupportsContextHistoryRule:   true,
		}
	case "openai":
		return ProviderCapabilities{
			SupportsResponsesInput:       false,
			SupportsPreviousResponseID:   false,
			SupportsStore:                false,
			SupportsInstructions:         false,
			SupportsToolChoice:           true,
			SupportsThinking:             false,
			DegradeOrphanToolCallsToText: true,
			InjectMissingToolResults:     true,
			SupportsContextHistoryRule:   false,
		}
	case "claude", "cli":
		return ProviderCapabilities{
			SupportsResponsesInput:       false,
			SupportsPreviousResponseID:   false,
			SupportsStore:                false,
			SupportsInstructions:         false,
			SupportsToolChoice:           true,
			SupportsThinking:             true,
			DegradeOrphanToolCallsToText: true,
			InjectMissingToolResults:     true,
			SupportsContextHistoryRule:   true,
		}
	default:
		return ProviderCapabilities{}
	}
}
