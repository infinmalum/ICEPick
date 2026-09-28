package main

import (
	"context"
	"errors"

	"icepick/internal/storage"
)

type AppStatus struct {
	Name           string `json:"name"`
	StorageReady   bool   `json:"storageReady"`
	AgentReady     bool   `json:"agentReady"`
	ModelConnected bool   `json:"modelConnected"`
	Message        string `json:"message"`
}

type StatusService struct {
	store   *storage.Store
	initErr error
}

func (s *StatusService) GetStatus() AppStatus {
	message := "模型未连接。配置与本地会话已就绪；模型适配将在 P2 接入。"
	if s.initErr != nil {
		message = "本地数据初始化失败：" + s.initErr.Error()
	}
	return AppStatus{Name: "ICEPick", StorageReady: s.initErr == nil && s.store != nil, AgentReady: false, ModelConnected: false, Message: message}
}

func (s *StatusService) checkReady() error {
	if s.initErr != nil {
		return s.initErr
	}
	if s.store == nil {
		return errors.New("本地数据库未就绪")
	}
	return nil
}

func (s *StatusService) ListSessions() ([]storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListSessions(context.Background())
}

func (s *StatusService) CreateSession() (storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return storage.Session{}, err
	}
	return s.store.CreateSession(context.Background(), "新会话")
}
