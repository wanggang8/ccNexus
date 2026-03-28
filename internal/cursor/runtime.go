package cursor

var defaultThinkingCache = NewThinkingCache()

func DefaultThinkingCache() *ThinkingCache {
	return defaultThinkingCache
}

func SetDefaultThinkingCacheForTest(cache *ThinkingCache) {
	if cache == nil {
		defaultThinkingCache = NewThinkingCache()
		return
	}
	defaultThinkingCache = cache
}
