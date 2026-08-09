package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libops/sitectl-libops/pkg/auth"
)

const (
	cacheDirName  = "cache"
	cacheValidity = 12 * time.Hour
)

// CacheKey represents a structured cache key
type CacheKey struct {
	ResourceType string // "organization", "project", "site"
	Operation    string // "list", "get"
	ParentType   string // optional: parent resource type
	ParentID     string // optional: parent resource ID
	SubResource  string // optional: "firewall", "members", "secrets"
	ResourceID   string // optional: specific resource ID
}

// GetCachePath returns the file path for a cache key
func (k CacheKey) GetCachePath() (string, error) {
	cacheDir, err := cacheRootPath()
	if err != nil {
		return "", err
	}

	parts := []string{
		cacheDir,
		safeCacheSegment(k.Operation),
	}

	if k.ParentType != "" && k.ParentID != "" {
		// Cached sub-resource: ~/.sitectl/cache/list/organization/<uuid>/firewall/list.resp
		parts = append(parts, safeCacheSegment(k.ParentType), safeCacheSegment(k.ParentID))
		if k.SubResource != "" {
			parts = append(parts, safeCacheSegment(k.SubResource))
		}
	} else {
		// Cached resource: ~/.sitectl/cache/list/organization/list.resp
		parts = append(parts, safeCacheSegment(k.ResourceType))
	}

	// Determine filename
	var filename string
	if k.ResourceID != "" {
		filename = fmt.Sprintf("%s.resp", safeCacheSegment(k.ResourceID))
	} else {
		filename = "list.resp"
	}

	parts = append(parts, filename)
	return filepath.Join(parts...), nil
}

// Get retrieves a cached value if it exists and is not expired
func Get(key CacheKey, target interface{}) (bool, error) {
	path, err := key.GetCachePath()
	if err != nil {
		return false, err
	}

	// Check if file exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Check if cache is expired
	if time.Since(info.ModTime()) > cacheValidity {
		// Cache expired, delete it
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	// Read cache file
	// #nosec G304 -- cache path is built under ~/.sitectl/cache from sanitized path segments.
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	// Unmarshal into target
	if err := json.Unmarshal(data, target); err != nil {
		// Cache corrupted, delete it
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	return true, nil
}

// Set stores a value in the cache
func Set(key CacheKey, value interface{}) error {
	path, err := key.GetCachePath()
	if err != nil {
		return err
	}

	// Create directory structure
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal value
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// Invalidate removes a cached value
func Invalidate(key CacheKey) error {
	path, err := key.GetCachePath()
	if err != nil {
		return err
	}

	// Remove file if it exists
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// InvalidatePattern removes all cache entries matching a pattern
// This is useful for invalidating all caches related to a resource
func InvalidatePattern(resourceType, resourceID string) error {
	cacheDir, err := cacheRootPath()
	if err != nil {
		return err
	}

	// Invalidate list cache for this resource type
	listKey := CacheKey{
		ResourceType: resourceType,
		Operation:    "list",
	}
	err = Invalidate(listKey)
	if err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	// If we have a specific resource ID, invalidate its get cache and all sub-resources
	if resourceID != "" {
		getKey := CacheKey{
			ResourceType: resourceType,
			Operation:    "get",
			ResourceID:   resourceID,
		}
		err = Invalidate(getKey)
		if err != nil {
			return fmt.Errorf("failed to invalidate cache: %w", err)
		}

		// Invalidate all sub-resource caches
		subResources := []string{"firewall", "members", "secrets"}
		for _, subResource := range subResources {
			subCacheDir := filepath.Join(cacheDir, "list", safeCacheSegment(resourceType), safeCacheSegment(resourceID), safeCacheSegment(subResource))
			if err := os.RemoveAll(subCacheDir); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// Clear removes all cached data
func Clear() error {
	cachePath, err := cacheRootPath()
	if err != nil {
		return err
	}

	if err := os.RemoveAll(cachePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func cacheRootPath() (string, error) {
	configDir, err := auth.ConfigDirPath()
	if err != nil {
		return "", fmt.Errorf("failed to resolve sitectl LibOps config directory: %w", err)
	}
	return filepath.Join(configDir, cacheDirName), nil
}

// HashID creates a short hash for cache keys (for very long IDs)
func HashID(id string) string {
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes (16 hex chars)
}

func safeCacheSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' ||
			r == '_' ||
			r == '.' {
			continue
		}
		return HashID(value)
	}
	if value == "." || value == ".." {
		return HashID(value)
	}
	return value
}
