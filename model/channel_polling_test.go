package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiKeyPollsEnabledKeys(t *testing.T) {
	withChannelCacheState(t)

	channel := &Channel{
		Id:  9101,
		Key: "key-1\nkey-2\nkey-3",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         3,
			MultiKeyStatusList:   map[int]int{1: common.ChannelStatusManuallyDisabled},
			MultiKeyPollingIndex: 0,
		},
	}
	channelsIDM[channel.Id] = channel

	key, index, apiErr := channel.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, 0, index)

	key, index, apiErr = channel.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-3", key)
	assert.Equal(t, 2, index)

	key, index, apiErr = channel.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, 0, index)
}

func TestDefaultMultiKeyPollingUpdatesCachedIndexForDetachedChannel(t *testing.T) {
	withChannelCacheState(t)

	cachedChannel := &Channel{
		Id:  9102,
		Key: "key-1\nkey-2",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	channelsIDM[cachedChannel.Id] = cachedChannel

	detachedChannel := *cachedChannel
	key, index, apiErr := detachedChannel.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, 0, index)
	assert.Equal(t, 1, cachedChannel.ChannelInfo.MultiKeyPollingIndex)

	anotherDetachedChannel := *cachedChannel
	anotherDetachedChannel.ChannelInfo.MultiKeyPollingIndex = 0
	key, index, apiErr = anotherDetachedChannel.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-2", key)
	assert.Equal(t, 1, index)
	assert.Equal(t, 0, cachedChannel.ChannelInfo.MultiKeyPollingIndex)
}

func TestDefaultChannelSelectionPollsByGroupAndModel(t *testing.T) {
	withChannelCacheState(t)

	group2model2channels = map[string]map[string][]int{
		"default": {
			"gpt-test": []int{101, 102},
		},
	}
	channelsIDM = map[int]*Channel{
		101: &Channel{Id: 101, Status: common.ChannelStatusEnabled},
		102: &Channel{Id: 102, Status: common.ChannelStatusEnabled},
	}

	channel, err := GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 101, channel.Id)

	channel, err = GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 102, channel.Id)

	channel, err = GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 101, channel.Id)
}

func TestDefaultChannelSelectionSkipsDisabledChannelsInCacheIndex(t *testing.T) {
	withChannelCacheState(t)

	group2model2channels = map[string]map[string][]int{
		"default": {
			"gpt-test": []int{101, 102},
		},
	}
	channelsIDM = map[int]*Channel{
		101: &Channel{Id: 101, Status: common.ChannelStatusManuallyDisabled},
		102: &Channel{Id: 102, Status: common.ChannelStatusEnabled},
	}

	channel, err := GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 102, channel.Id)

	channelsIDM[102].Status = common.ChannelStatusAutoDisabled
	channel, err = GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestSameModelPollsDifferentChannelsAndSameChannelDifferentKeys(t *testing.T) {
	withChannelCacheState(t)

	highPriority := int64(100)
	lowPriority := int64(10)
	group2model2channels = map[string]map[string][]int{
		"default": {
			"banana-pro": []int{102, 101},
		},
	}
	channelsIDM = map[int]*Channel{
		101: &Channel{
			Id:       101,
			Name:     "banana-compatible",
			Status:   common.ChannelStatusEnabled,
			Priority: &highPriority,
			Key:      "channel-101-key-1\nchannel-101-key-2",
			ChannelInfo: ChannelInfo{
				IsMultiKey:   true,
				MultiKeySize: 2,
			},
		},
		102: &Channel{
			Id:       102,
			Name:     "banana-compatible",
			Status:   common.ChannelStatusEnabled,
			Priority: &lowPriority,
			Key:      "channel-102-key-1\nchannel-102-key-2",
			ChannelInfo: ChannelInfo{
				IsMultiKey:   true,
				MultiKeySize: 2,
			},
		},
	}

	var channelIds []int
	var keys []string
	var keyIndexes []int
	for i := 0; i < 6; i++ {
		channel, err := GetRandomSatisfiedChannel("default", "banana-pro", 0)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "banana-compatible", channel.Name)

		key, index, apiErr := channel.GetNextEnabledKey()
		require.Nil(t, apiErr)

		channelIds = append(channelIds, channel.Id)
		keys = append(keys, key)
		keyIndexes = append(keyIndexes, index)
	}

	assert.Equal(t, []int{101, 102, 101, 102, 101, 102}, channelIds)
	assert.Equal(t, []string{
		"channel-101-key-1",
		"channel-102-key-1",
		"channel-101-key-2",
		"channel-102-key-2",
		"channel-101-key-1",
		"channel-102-key-1",
	}, keys)
	assert.Equal(t, []int{0, 0, 1, 1, 0, 0}, keyIndexes)
	assert.Equal(t, 1, channelsIDM[101].ChannelInfo.MultiKeyPollingIndex)
	assert.Equal(t, 1, channelsIDM[102].ChannelInfo.MultiKeyPollingIndex)
}

func TestDBChannelSelectionPollsPrioritySortedChannels(t *testing.T) {
	truncateTables(t)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGroup2Model2PollingIndex := group2model2pollingIndex
	common.MemoryCacheEnabled = false
	group2model2pollingIndex = map[string]map[string]int{}
	initCol()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		group2model2pollingIndex = oldGroup2Model2PollingIndex
	})

	highPriority := int64(100)
	lowPriority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id:       101,
		Type:     1,
		Key:      "channel-101-key",
		Group:    "default",
		Models:   "banana-pro",
		Status:   common.ChannelStatusEnabled,
		Priority: &highPriority,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:       102,
		Type:     1,
		Key:      "channel-102-key",
		Group:    "default",
		Models:   "banana-pro",
		Status:   common.ChannelStatusEnabled,
		Priority: &lowPriority,
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "banana-pro", ChannelId: 102, Enabled: true, Priority: &lowPriority},
		{Group: "default", Model: "banana-pro", ChannelId: 101, Enabled: true, Priority: &highPriority},
	}).Error)

	var channelIds []int
	for i := 0; i < 4; i++ {
		channel, err := GetRandomSatisfiedChannel("default", "banana-pro", 0)
		require.NoError(t, err)
		require.NotNil(t, channel)
		channelIds = append(channelIds, channel.Id)
	}

	assert.Equal(t, []int{101, 102, 101, 102}, channelIds)
}

func TestDBChannelSelectionSkipsDisabledChannelWithStaleAbility(t *testing.T) {
	truncateTables(t)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGroup2Model2PollingIndex := group2model2pollingIndex
	common.MemoryCacheEnabled = false
	group2model2pollingIndex = map[string]map[string]int{}
	initCol()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		group2model2pollingIndex = oldGroup2Model2PollingIndex
	})

	highPriority := int64(100)
	lowPriority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id:       201,
		Type:     1,
		Key:      "disabled-key",
		Group:    "default",
		Models:   "banana-pro",
		Status:   common.ChannelStatusManuallyDisabled,
		Priority: &highPriority,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:       202,
		Type:     1,
		Key:      "enabled-key",
		Group:    "default",
		Models:   "banana-pro",
		Status:   common.ChannelStatusEnabled,
		Priority: &lowPriority,
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "banana-pro", ChannelId: 201, Enabled: true, Priority: &highPriority},
		{Group: "default", Model: "banana-pro", ChannelId: 202, Enabled: true, Priority: &lowPriority},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("default", "banana-pro", 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 202, channel.Id)

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", 202).Update("status", common.ChannelStatusAutoDisabled).Error)
	channel, err = GetRandomSatisfiedChannel("default", "banana-pro", 0)
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestCacheUpdateChannelRefreshesGroupModelIndex(t *testing.T) {
	withChannelCacheState(t)

	oldChannel := &Channel{
		Id:     201,
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "old-model",
	}
	channelsIDM[oldChannel.Id] = oldChannel
	group2model2channels = map[string]map[string][]int{
		"default": {
			"old-model": []int{oldChannel.Id},
		},
	}

	updatedChannel := &Channel{
		Id:     oldChannel.Id,
		Status: common.ChannelStatusEnabled,
		Group:  "vip",
		Models: "new-model",
	}
	CacheUpdateChannel(updatedChannel)

	assert.Empty(t, group2model2channels["default"]["old-model"])
	assert.Equal(t, []int{oldChannel.Id}, group2model2channels["vip"]["new-model"])

	updatedChannel.Status = common.ChannelStatusManuallyDisabled
	CacheUpdateChannel(updatedChannel)

	assert.Empty(t, group2model2channels["vip"]["new-model"])
}

func TestUpdateChannelStatusRefreshesMultiKeyCacheIndex(t *testing.T) {
	truncateTables(t)
	withChannelCacheState(t)
	initCol()

	channel := Channel{
		Id:     301,
		Type:   1,
		Key:    "key-1\nkey-2",
		Group:  "default",
		Models: "gpt-test",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-test",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	cachedChannel := channel
	channelsIDM[channel.Id] = &cachedChannel
	group2model2channels = map[string]map[string][]int{
		"default": {
			"gpt-test": []int{channel.Id},
		},
	}

	require.True(t, UpdateChannelStatus(channel.Id, "key-1", common.ChannelStatusAutoDisabled, "bad key"))
	assert.Equal(t, common.ChannelStatusEnabled, channelsIDM[channel.Id].Status)
	assert.Equal(t, []int{channel.Id}, group2model2channels["default"]["gpt-test"])

	require.True(t, UpdateChannelStatus(channel.Id, "key-2", common.ChannelStatusAutoDisabled, "bad key"))
	assert.Equal(t, common.ChannelStatusAutoDisabled, channelsIDM[channel.Id].Status)
	assert.Empty(t, group2model2channels["default"]["gpt-test"])

	selected, err := GetRandomSatisfiedChannel("default", "gpt-test", 0)
	require.NoError(t, err)
	assert.Nil(t, selected)

	require.True(t, UpdateChannelStatus(channel.Id, "key-1", common.ChannelStatusEnabled, ""))
	assert.Equal(t, common.ChannelStatusEnabled, channelsIDM[channel.Id].Status)
	assert.Equal(t, []int{channel.Id}, group2model2channels["default"]["gpt-test"])
}

func TestChannelUpdatePersistsEmptyEditableFieldsAndKeepsKeyWhenEmpty(t *testing.T) {
	truncateTables(t)

	channel := Channel{
		Type:   1,
		Key:    "secret-key",
		Name:   "before",
		Other:  "before-other",
		Models: "old-model",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())

	empty := ""
	updated := Channel{
		Id:                channel.Id,
		Type:              channel.Type,
		Key:               "",
		Name:              "",
		Other:             "",
		Models:            "new-model",
		Group:             "default",
		ModelMapping:      &empty,
		StatusCodeMapping: &empty,
		OtherSettings:     "",
		ChannelInfo:       channel.ChannelInfo,
	}
	require.NoError(t, updated.Update())

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "secret-key", reloaded.Key)
	assert.Equal(t, "", reloaded.Name)
	assert.Equal(t, "", reloaded.Other)
	assert.Equal(t, "new-model", reloaded.Models)
	assert.Equal(t, "", reloaded.GetModelMapping())
	assert.Equal(t, "", reloaded.GetStatusCodeMapping())
	assert.Equal(t, "", reloaded.OtherSettings)
}

func TestUpdateChannelPartialOnlyUpdatesProvidedFields(t *testing.T) {
	truncateTables(t)

	baseURL := "https://upstream.example.com"
	remark := "keep remark"
	priority := int64(1)
	weight := uint(2)
	channel := Channel{
		Type:     1,
		Key:      "secret-key",
		Name:     "keep-name",
		BaseURL:  &baseURL,
		Remark:   &remark,
		Other:    "keep-other",
		Models:   "old-model",
		Group:    "default",
		Status:   common.ChannelStatusEnabled,
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, channel.Insert())

	nextPriority := int64(9)
	nextWeight := uint(7)
	emptyBaseURL := ""
	updated, err := UpdateChannelPartialById(
		channel.Id,
		map[string]interface{}{
			"priority": nextPriority,
			"weight":   nextWeight,
			"base_url": emptyBaseURL,
		},
		false,
		nil,
		&nextPriority,
		&nextWeight,
	)
	require.NoError(t, err)
	require.NotNil(t, updated)

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "secret-key", reloaded.Key)
	assert.Equal(t, "keep-name", reloaded.Name)
	assert.Equal(t, "keep-other", reloaded.Other)
	assert.Equal(t, "old-model", reloaded.Models)
	assert.Equal(t, "default", reloaded.Group)
	require.NotNil(t, reloaded.BaseURL)
	assert.Equal(t, "", *reloaded.BaseURL)
	assert.Equal(t, nextPriority, reloaded.GetPriority())
	assert.Equal(t, int(nextWeight), reloaded.GetWeight())

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, nextPriority, *ability.Priority)
	assert.Equal(t, nextWeight, ability.Weight)
}

func withChannelCacheState(t *testing.T) {
	t.Helper()

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGroup2Model2Channels := group2model2channels
	oldChannelsIDM := channelsIDM
	oldGroup2Model2PollingIndex := group2model2pollingIndex

	common.MemoryCacheEnabled = true
	group2model2channels = map[string]map[string][]int{}
	channelsIDM = map[int]*Channel{}
	group2model2pollingIndex = map[string]map[string]int{}

	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		group2model2channels = oldGroup2Model2Channels
		channelsIDM = oldChannelsIDM
		group2model2pollingIndex = oldGroup2Model2PollingIndex
	})
}
