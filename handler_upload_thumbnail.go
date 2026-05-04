package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	maxMemory := int64(10) << 20
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		fmt.Print(err)
		respondWithError(w, http.StatusInternalServerError, "Failed to Parse Thumbnail.", err)
		return 
	}

	fileBody, fileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Parse File", err)
		return
	}

	mediaType := fileHeader.Header.Get("Content-Type")
	fileExt := strings.Split(mediaType, "/")
	if fileExt[0] != "image" {
		respondWithError(w, http.StatusForbidden, "Images Only Buddy", err)
		return
	}

	magicFileName := make([]byte, 32)
	_, err = rand.Read(magicFileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Fill magicFileName.", err)
		return
	}

	encodedMagicFileName := base64.RawURLEncoding.EncodeToString(magicFileName)

	imgFileName := fmt.Sprintf("%s.%s", encodedMagicFileName, fileExt[1])
	imgFilePath := filepath.Join(cfg.assetsRoot, imgFileName)
	img, err := os.Create(imgFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed To Create Image File on Disk.", err)
		return
	}

	_, err = io.Copy(img, fileBody)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Copy Image to Disk.", err)
		return 
	}


	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error Fetching Video", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "No Touchy. Not Your Video.", err)
		return
	}

	imgURL := fmt.Sprintf("http://localhost:%v/assets/%v", cfg.port, imgFileName)
	dbVideo.ThumbnailURL = &imgURL
	
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Update Video.", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
