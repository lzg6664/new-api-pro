package constant

type ChannelSelectionMode string

const (
	ChannelSelectionModeWeightedRandom ChannelSelectionMode = "weighted_random"
	ChannelSelectionModePolling        ChannelSelectionMode = "polling"
)
