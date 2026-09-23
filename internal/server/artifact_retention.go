package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gorm.io/gorm"
)

func (a *App) projectArtifactBytes(projectID uint) int64 {
	var total int64
	a.db.Model(&ArtifactFile{}).
		Joins("JOIN releases ON releases.id = artifact_files.release_id").
		Where("releases.project_id = ?", projectID).
		Select("COALESCE(SUM(artifact_files.size), 0)").
		Scan(&total)
	return total
}

func (a *App) enforceProjectArtifactCapacity(projectID, preferredReleaseID uint) {
	var project Project
	if a.db.First(&project, projectID).Error != nil || project.Type != "artifact" || project.MaxArtifactBytes <= 0 {
		return
	}
	var newest Release
	if a.db.Where("project_id = ?", projectID).Order("created_at DESC").Order("id DESC").First(&newest).Error != nil {
		return
	}
	if preferredReleaseID == 0 {
		preferredReleaseID = newest.ID
	}
	var releases []Release
	a.db.Where("project_id = ? AND id <> ?", projectID, preferredReleaseID).Order("created_at ASC").Order("id ASC").Find(&releases)
	for _, release := range releases {
		if a.projectArtifactBytes(projectID) <= project.MaxArtifactBytes {
			return
		}
		var pendingEvents, activeRuns int64
		a.db.Model(&ReleaseEvent{}).Where("release_id = ? AND processed_at IS NULL", release.ID).Count(&pendingEvents)
		a.db.Model(&WorkflowRun{}).Where("release_id = ? AND status IN ?", release.ID, []string{"queued", "running"}).Count(&activeRuns)
		if pendingEvents > 0 || activeRuns > 0 {
			continue
		}
		_ = a.deleteReleaseForRetention(projectID, release.ID)
	}
}

func (a *App) deleteReleaseForRetention(projectID, releaseID uint) error {
	root := filepath.Join(a.cfg.StorageDir, strconv.FormatUint(uint64(projectID), 10), strconv.FormatUint(uint64(releaseID), 10))
	trash := root + fmt.Sprintf(".retention-%d", time.Now().UnixNano())
	moved := false
	if _, err := os.Stat(root); err == nil {
		if err = os.Rename(root, trash); err != nil {
			return err
		}
		moved = true
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("release_id = ?", releaseID).Delete(&ArtifactAccessLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("release_id = ?", releaseID).Delete(&ReleaseEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("release_id = ?", releaseID).Delete(&ArtifactFile{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ?", releaseID, projectID).Delete(&Release{}).Error
	})
	if err != nil {
		if moved {
			_ = os.Rename(trash, root)
		}
		return err
	}
	if moved {
		return os.RemoveAll(trash)
	}
	return nil
}
