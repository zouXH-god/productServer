package main

import (
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	projectView = iota
	projectDevelop
	projectAdmin
	projectOwn
)

func roleLevel(role string) int {
	switch role {
	case "owner":
		return projectOwn
	case "admin":
		return projectAdmin
	case "developer":
		return projectDevelop
	default:
		return projectView
	}
}

func (a *App) projectAccess(c *gin.Context, id uint, required int) (Project, ProjectMember, bool) {
	uid := c.MustGet("userID").(uint)
	var project Project
	if a.db.First(&project, id).Error != nil {
		fail(c, 404, "not_found", "project not found")
		return project, ProjectMember{}, false
	}
	var member ProjectMember
	if a.db.Where("project_id=? AND user_id=?", id, uid).First(&member).Error != nil {
		fail(c, 404, "not_found", "project not found")
		return project, member, false
	}
	if roleLevel(member.Role) < required {
		fail(c, 403, "forbidden", "insufficient project permission")
		return project, member, false
	}
	c.Set("projectRole", member.Role)
	return project, member, true
}

func (a *App) listProjectMembers(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, id, projectView); !ok {
		return
	}
	var members []ProjectMember
	a.db.Preload("User").Where("project_id=?", id).Order("id").Find(&members)
	c.JSON(200, members)
}
func (a *App) addProjectMember(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	_, actor, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	var in struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "username and role required")
		return
	}
	if in.Role != "admin" && in.Role != "developer" && in.Role != "viewer" {
		fail(c, 400, "invalid_role", "invalid project role")
		return
	}
	if actor.Role == "admin" && in.Role == "admin" {
		fail(c, 403, "forbidden", "only owner can add administrators")
		return
	}
	var user User
	if a.db.Where("username=? AND enabled=?", strings.TrimSpace(in.Username), true).First(&user).Error != nil {
		fail(c, 404, "not_found", "user not found")
		return
	}
	member := ProjectMember{ProjectID: id, UserID: user.ID, Role: in.Role}
	if a.db.Create(&member).Error != nil {
		fail(c, 409, "member_exists", "user is already a project member")
		return
	}
	a.db.Preload("User").First(&member, member.ID)
	c.JSON(201, member)
}
func (a *App) updateProjectMember(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	_, actor, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	uid, ok := parseID(c, "userId")
	if !ok {
		return
	}
	var target ProjectMember
	if a.db.Where("project_id=? AND user_id=?", id, uid).First(&target).Error != nil {
		fail(c, 404, "not_found", "member not found")
		return
	}
	if target.Role == "owner" {
		fail(c, 400, "owner_protected", "transfer ownership instead")
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if c.ShouldBindJSON(&in) != nil || (in.Role != "admin" && in.Role != "developer" && in.Role != "viewer") {
		fail(c, 400, "invalid_role", "invalid project role")
		return
	}
	if actor.Role == "admin" && (target.Role == "admin" || in.Role == "admin") {
		fail(c, 403, "forbidden", "only owner can manage administrators")
		return
	}
	a.db.Model(&target).Update("role", in.Role)
	a.db.Preload("User").First(&target, target.ID)
	c.JSON(200, target)
}
func (a *App) deleteProjectMember(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	_, actor, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	uid, ok := parseID(c, "userId")
	if !ok {
		return
	}
	var target ProjectMember
	if a.db.Where("project_id=? AND user_id=?", id, uid).First(&target).Error != nil {
		fail(c, 404, "not_found", "member not found")
		return
	}
	if target.Role == "owner" {
		fail(c, 400, "owner_protected", "owner cannot be removed")
		return
	}
	if actor.Role == "admin" && target.Role == "admin" {
		fail(c, 403, "forbidden", "only owner can remove administrators")
		return
	}
	a.db.Delete(&target)
	c.Status(204)
}
func (a *App) transferProjectOwner(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, old, ok := a.projectAccess(c, id, projectOwn)
	if !ok {
		return
	}
	var in struct {
		UserID uint `json:"user_id"`
	}
	if c.ShouldBindJSON(&in) != nil || in.UserID == 0 {
		fail(c, 400, "invalid_request", "user_id required")
		return
	}
	var next ProjectMember
	if a.db.Where("project_id=? AND user_id=?", id, in.UserID).First(&next).Error != nil {
		fail(c, 404, "not_found", "target must be a project member")
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Project{}).Where("id=?", id).Update("user_id", in.UserID).Error; err != nil {
			return err
		}
		if err := tx.Model(&ProjectMember{}).Where("id=?", old.ID).Update("role", "admin").Error; err != nil {
			return err
		}
		return tx.Model(&ProjectMember{}).Where("id=?", next.ID).Update("role", "owner").Error
	})
	if err != nil {
		fail(c, 409, "transfer_failed", err.Error())
		return
	}
	project.UserID = in.UserID
	c.JSON(200, project)
}
