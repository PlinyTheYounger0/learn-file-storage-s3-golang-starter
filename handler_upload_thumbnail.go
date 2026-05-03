package main

import (
	"fmt"
	"net/http"
	"io"

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

	// TODO: implement the upload here
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
	image_data, err := io.ReadAll(fileBody)

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error Fetching Video", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "No Touchy. Not Your Video.", err)
		return
	}

	thumbnail := thumbnail{
		data: image_data,
		mediaType: mediaType,
	}

	videoThumbnails[videoID] = thumbnail
	
	thumbnailURL := fmt.Sprintf("http://localhost:%v/api/thumbnails/%v", cfg.port, videoIDString)
	dbVideo.ThumbnailURL = &thumbnailURL
	
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Update Video.", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
