package service

import (
	"a21hc3NpZ25tZW50/model"
	repo "a21hc3NpZ25tZW50/repository"
	"a21hc3NpZ25tZW50/utils"
	"bytes"
	"errors"
	"fmt"
	"text/template"
	"time"

	"github.com/golang-jwt/jwt"
)

type UserService interface {
	Register(user *model.User) (model.User, error)
	Login(user *model.User) (token *string, err error)
	GetUserTaskCategory(userID int) ([]model.UserTaskCategory, error)
	GetUserProfile(userID int) (model.UserProfile, error)
	SendKey(srcUserID int, dstUserID int) error
}

type userService struct {
	userRepo     repo.UserRepository
	sessionsRepo repo.SessionRepository
}

func NewUserService(userRepository repo.UserRepository, sessionsRepo repo.SessionRepository) UserService {
	return &userService{userRepository, sessionsRepo}
}

func (s *userService) Register(user *model.User) (model.User, error) {
	dbUser, err := s.userRepo.GetUserByEmail(user.Email)
	if err != nil {
		return *user, err
	}

	// user.Password = utils.EncryptAES(user.Password)
	// user.IDCard = utils.EncryptAES(user.IDCard)
	user.IDCard, err = utils.EncryptRC4(user.IDCard)
	if err != nil {
		return *user, errors.New("error encrypting id card")
	}
	user.Address, err = utils.EncryptRC4(user.Address)
	// fmt.Printf("user address: %+v\n", user.Address)
	if err != nil {
		return *user, errors.New("error encrypting address")
	}
	// user.NIK, err = utils.EncryptDES(user.NIK)
	user.NIK = utils.EncryptAES(user.NIK)
	// fmt.Printf("user nik: %+v\n", user.NIK)
	user.Password, err = utils.HashPassword(user.Password)
	if err != nil {
		return *user, errors.New("error hashing password")
	}

	if dbUser.Email != "" || dbUser.ID != 0 {
		return *user, errors.New("email already exists")
	}

	PrivateKey, PublicKey, err := utils.GenerateKeyPair()
	if err != nil {
		return *user, errors.New("error generating key pair")
	}

	// * PKey Encrypt for safe storage
	// PrivateKey = utils.EncryptAES(PrivateKey, hashedPassword)

	user.PrivateKey = PrivateKey
	user.PublicKey = PublicKey

	user.CreatedAt = time.Now()

	newUser, err := s.userRepo.CreateUser(*user)
	if err != nil {
		return *user, err
	}

	return newUser, nil
}

func (s *userService) Login(user *model.User) (token *string, err error) {
	dbUser, err := s.userRepo.GetUserByEmail(user.Email)
	if err != nil {
		return nil, err
	}

	if dbUser.Email == "" || dbUser.ID == 0 {
		return nil, errors.New("user not found")
	}

	fmt.Printf("dbUser password: %+v\n", dbUser.Password)

	Verified := utils.VerifyPassword(dbUser.Password, user.Password)

	fmt.Printf("decrypt dbUser password: %+v\n", dbUser.Password)
	fmt.Printf("user password: %+v\n", user.Password)

	if Verified {
		return nil, errors.New("wrong email or password")
	}

	expirationTime := time.Now().Add(20 * time.Minute)
	claims := &model.Claims{
		ID:    dbUser.ID,
		Email: dbUser.Email,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := t.SignedString(model.JwtKey)
	if err != nil {
		return nil, err
	}

	session := model.Session{
		Token:  tokenString,
		Email:  user.Email,
		Expiry: expirationTime,
	}

	_, err = s.sessionsRepo.SessionAvailEmail(session.Email)
	if err != nil {
		err = s.sessionsRepo.AddSessions(session)
	} else {
		err = s.sessionsRepo.UpdateSessions(session)
	}

	return &tokenString, nil
}

func (s *userService) GetUserTaskCategory(userID int) ([]model.UserTaskCategory, error) {
	taskCategory, err := s.userRepo.GetUserTaskCategory(userID)
	for i := 0; i < len(taskCategory); i++ {
		// decryptFilePath, err := utils.DecryptAES(taskCategory[i].DocumentPath)
		decryptFilePath, err := utils.DecryptAES(taskCategory[i].DocumentPath)
		if err != nil {
			return nil, err
		}
		taskCategory[i].DocumentPath = decryptFilePath
	}

	if err != nil {
		return nil, err
	}
	return taskCategory, nil
}

func (s *userService) GetUserProfile(userID int) (model.UserProfile, error) {
	userData, err := s.userRepo.GetUserProfile(userID)

	if err != nil {
		return userData, err
	}

	userData.NIK, err = utils.DecryptAES(userData.NIK)
	if err != nil {
		return userData, err
	}
	userData.Address, err = utils.DecryptRC4(userData.Address)
	if err != nil {
		return userData, err
	}
	// userData.IDCard, err = utils.DecryptAES(userData.IDCard)
	userData.IDCard, err = utils.DecryptRC4(userData.IDCard)
	if err != nil {
		return userData, err
	}

	return userData, nil
}

func (s *userService) SendKey(srcUserID int, dstUserID int) error {
	dstUserCreds, err := s.userRepo.GetUserPublicCredentials(dstUserID)
	if err != nil {
		return err
	}

	srcUserCreds, err := s.userRepo.GetUserPublicCredentials(srcUserID)
	if err != nil {
		return err
	}

	// * Access Link (?)
	// -----

	// * Symmetric Key Generation
	key := utils.GenerateKey(32)

	// Asymmetric Encryption
	EncryptedKeyString, err := utils.EncryptRSA(key, dstUserCreds.PublicKey)
	if err != nil {
		return err
	}

	// Mail template
	const tpl = `
	<!DOCTYPE html>
	<html>
	<head>
	  <title>Notification of Received Key String</title>
	</head>
	<body>
	  <h1>Notification of Received Key String</h1>
	  <p>Dear {{.Recipient}},</p>
	  <p>We are writing to inform you that we have received a key string from {{.Sender}}.</p>
	  <p>Here are the details of the key string:</p>
	  
	  <p>Ecrypted Key String: {{.KeyString}}</li></p>
	  
	  <p>Best Regards,</p>
	  <p>{{.Sender}}<br>
	</body>
	</html>
	`

	data := map[string]string{
		"Sender":    srcUserCreds.Fullname,
		"Recipient": dstUserCreds.Fullname,
		"KeyString": EncryptedKeyString,
	}

	t := template.Must(template.New("email").Parse(tpl))

	var buf bytes.Buffer

	if err := t.Execute(&buf, data); err != nil {
		return err
	}

	subject := "Notification of Received Key String"
	body := buf.String()

	err = utils.SendEmail(dstUserCreds.Email, subject, body)
	if err != nil {
		return err
	}

	return nil
}
