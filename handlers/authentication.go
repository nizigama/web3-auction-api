package handlers

import (
	"Web3AuctionApi/models"
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"log"
	"time"
)

type registerRequest struct {
	Username             string `validate:"required,email" json:"username"`
	Password             string `validate:"required,min=6" json:"password"`
	PasswordConfirmation string `validate:"required,min=6,eqfield=Password" json:"passwordConfirmation"`
}

type loginRequest struct {
	Username string `validate:"required,email" json:"username"`
	Password string `validate:"required,min=6" json:"password"`
}

// Register func for registration
// @Description Register.
// @Summary Register
// @Tags auth
// @Accept json
// @Produce json
// @Param request body registerRequest true "New user"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string "Validation failed"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /register [post]
func (ah *AuthHandler) Register(c *fiber.Ctx) error {

	registrationData := registerRequest{}

	err := c.BodyParser(&registrationData)
	if err != nil {
		return errorResponse(c, fiber.StatusBadRequest, err.Error(), nil)
	}

	validationErrors := validateRequest(registrationData)
	if validationErrors != nil {
		return errorResponse(c, fiber.StatusBadRequest, "Validation failed", validationErrors)
	}

	var existingUsername int64

	err = ah.db.Model(&models.User{}).Where("username = ?", registrationData.Username).Count(&existingUsername).Error
	if err != nil {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Failed creating new user", nil)
	}

	if existingUsername != 0 {
		return errorResponse(c, fiber.StatusForbidden, "Username already in use", nil)
	}

	hashedP, err := bcrypt.GenerateFromPassword([]byte(registrationData.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Failed creating new user", nil)
	}

	user := models.User{Username: registrationData.Username, Password: string(hashedP)}

	err = ah.db.Create(&user).Error
	if err != nil {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Failed creating new user", nil)
	}

	return successResponse(c, map[string]string{
		"message": "Registration successful",
	})
}

// Login func for login
// @Description Login.
// @Summary Login
// @Tags auth
// @Accept json
// @Produce json
// @Param request body loginRequest true "Login user"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string "Validation failed"
// @Failure 403 {object} map[string]string "Credentials not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /login [post]
func (ah *AuthHandler) Login(c *fiber.Ctx) error {

	loginData := loginRequest{}

	err := c.BodyParser(&loginData)
	if err != nil {
		return errorResponse(c, fiber.StatusBadRequest, err.Error(), nil)
	}

	validationErrors := validateRequest(loginData)
	if validationErrors != nil {
		return errorResponse(c, fiber.StatusBadRequest, "Validation failed", validationErrors)
	}

	var user models.User

	err = ah.db.First(&user, "username = ?", loginData.Username).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Server error", nil)
	}

	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return errorResponse(c, fiber.StatusForbidden, "Credentials not found", nil)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(loginData.Password))
	if err != nil {
		return errorResponse(c, fiber.StatusForbidden, "Credentials not found", nil)
	}

	// Create the Claims
	claims := jwt.MapClaims{
		"username": user.Username,
		"exp":      time.Now().Add(time.Second * time.Duration(ah.tokenDuration)).Unix(),
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Generate encoded token and send it as response.
	t, err := token.SignedString([]byte(ah.authSecret))
	if err != nil {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Server error", nil)
	}

	return successResponse(c, map[string]string{
		"token": t,
	})
}

// Logout func for logout
// @Description Logout.
// @Summary Logout
// @Tags auth
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} map[string]string
// @Failure 403 {object} map[string]string "Unauthenticated."
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /logout [post]
func (ah *AuthHandler) Logout(c *fiber.Ctx) error {

	user := c.Locals("user").(*jwt.Token)
	claims := user.Claims.(jwt.MapClaims)
	username := claims["username"].(string)

	invalidToken := models.InvalidToken{
		Username: username,
		Token:    user.Raw,
	}

	err := ah.db.Create(&invalidToken).Error
	if err != nil {
		log.Println(err)
		return errorResponse(c, fiber.StatusInternalServerError, "Server error", nil)
	}

	return successResponse(c, map[string]string{
		"message": "Token invalidated successfully",
	})
}
