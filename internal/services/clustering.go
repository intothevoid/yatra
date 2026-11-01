package services

import (
	"context"
	"fmt"
	"image"
	"os"
	"sort"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Clusterer finds duplicate and similar photos in a trip.
type Clusterer struct {
	store *store.Store
}

// NewClusterer creates a new clusterer.
func NewClusterer(s *store.Store) *Clusterer {
	return &Clusterer{store: s}
}

// Cluster finds duplicate and similar photos in a trip.
// Returns groups sorted by size (largest first).
func (c *Clusterer) Cluster(ctx context.Context, tripID string) ([]store.PhotoGroup, error) {
	photos, err := c.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get photos: %w", err)
	}
	if len(photos) == 0 {
		return nil, nil
	}

	// Phase 1: Exact duplicates by source file ID
	exactGroups := c.findExactDuplicates(photos)

	// Phase 2: Near-duplicates by perceptual hash
	similarGroups := c.findSimilarPhotos(ctx, photos)

	// Merge and deduplicate
	allGroups := mergeGroups(exactGroups, similarGroups, tripID)

	// Store in database
	for i := range allGroups {
		allGroups[i].ID = fmt.Sprintf("pg_%s_%s_%d", tripID, allGroups[i].Type, i)
		if err := c.store.SavePhotoGroup(ctx, allGroups[i]); err != nil {
			return nil, fmt.Errorf("store group: %w", err)
		}
	}

	// Sort by size (largest first)
	sort.Slice(allGroups, func(i, j int) bool {
		return len(allGroups[i].Photos) > len(allGroups[j].Photos)
	})

	return allGroups, nil
}

// findExactDuplicates groups photos by source file ID.
func (c *Clusterer) findExactDuplicates(photos []domain.Photo) []store.PhotoGroup {
	groups := make(map[string][]string)
	for _, p := range photos {
		if p.SourceFileID != "" {
			groups[p.SourceFileID] = append(groups[p.SourceFileID], p.ID)
		}
	}

	var result []store.PhotoGroup
	for _, ids := range groups {
		if len(ids) > 1 {
			result = append(result, store.PhotoGroup{
				Type:   "duplicate",
				Photos: ids,
			})
		}
	}
	return result
}

// findSimilarPhotos groups photos by perceptual hash (Hamming distance ≤ 10).
func (c *Clusterer) findSimilarPhotos(ctx context.Context, photos []domain.Photo) []store.PhotoGroup {
	type photoHash struct {
		id   string
		hash uint64
	}
	var hashes []photoHash
	for _, p := range photos {
		h, err := perceptualHash(ctx, p.ThumbnailPath)
		if err != nil {
			continue
		}
		hashes = append(hashes, photoHash{id: p.ID, hash: h})
	}

	visited := make(map[string]bool)
	var groups []store.PhotoGroup
	for i := 0; i < len(hashes); i++ {
		if visited[hashes[i].id] {
			continue
		}
		var group []string
		for j := i; j < len(hashes); j++ {
			if visited[hashes[j].id] {
				continue
			}
			if hammingDistance(hashes[i].hash, hashes[j].hash) <= 10 {
				group = append(group, hashes[j].id)
				visited[hashes[j].id] = true
			}
		}
		if len(group) > 1 {
			groups = append(groups, store.PhotoGroup{
				Type:   "similar",
				Photos: group,
			})
		}
	}
	return groups
}

// perceptualHash computes an average hash (aHash) for an image.
func perceptualHash(ctx context.Context, imagePath string) (uint64, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	var pixels []float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			sx := bounds.Min.X + x*srcW/8
			sy := bounds.Min.Y + y*srcH/8
			c := img.At(sx, sy)
			r, g, b, _ := c.RGBA()
			lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
			pixels = append(pixels, lum/65535.0)
		}
	}

	var sum float64
	for _, p := range pixels {
		sum += p
	}
	mean := sum / float64(len(pixels))

	var hash uint64
	for i, p := range pixels {
		if p > mean {
			hash |= (1 << uint(63-i))
		}
	}

	return hash, nil
}

// hammingDistance counts differing bits between two 64-bit values.
func hammingDistance(a, b uint64) int {
	x := a ^ b
	count := 0
	for x != 0 {
		x &= x - 1
		count++
	}
	return count
}

// mergeGroups combines exact and similar groups, avoiding duplicates.
func mergeGroups(exact, similar []store.PhotoGroup, tripID string) []store.PhotoGroup {
	clustered := make(map[string]bool)
	for _, g := range exact {
		for _, id := range g.Photos {
			clustered[id] = true
		}
	}

	var filteredSimilar []store.PhotoGroup
	for _, g := range similar {
		var photos []string
		for _, id := range g.Photos {
			if !clustered[id] {
				photos = append(photos, id)
			}
		}
		if len(photos) > 1 {
			g.Photos = photos
			g.TripID = tripID
			filteredSimilar = append(filteredSimilar, g)
		}
	}

	var result []store.PhotoGroup
	for _, g := range exact {
		g.TripID = tripID
		result = append(result, g)
	}
	result = append(result, filteredSimilar...)
	return result
}
