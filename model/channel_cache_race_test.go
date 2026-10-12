package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Channels and abilities are read in separate queries. A sync that sees an
// enabled channel whose group is missing from abilities (abilities being
// rewritten) crashed the process on 2026-10-11; it must keep serving instead.
func TestInitChannelCacheToleratesGroupMissingFromAbilities(t *testing.T) {
	truncateTables(t)
	originalMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCache; InitChannelCache() })

	channel := Channel{Id: 920001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "race", Models: "race-model", Group: "race-group"}
	require.NoError(t, channel.Insert())
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error)

	require.NotPanics(t, InitChannelCache)
	channelSyncLock.RLock()
	ids := group2model2channels["race-group"]["race-model"]
	channelSyncLock.RUnlock()
	assert.Equal(t, []int{channel.Id}, ids)
}
