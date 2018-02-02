package main

import (
  "log"

  "github.com/satori/go.uuid"
)

type event struct {
  sessionID uuid.UUID
  accessKey string
  category string
  size int64
}

func persist_event(sessionID uuid.UUID, accessKey string, category string, size int64) {
  e := event{
    sessionID,
    accessKey,
    category,
    size,
  }

  e.persist()
}

func (e *event) persist() {
  var sql = `
    INSERT INTO events
    (
      session_id,
      access_key_id,
      type,
      size
    )
    VALUES($1, $2, $3, $4);
  `
  _, err := db.Exec(sql, e.sessionID, e.accessKey, e.category, e.size)
  if err != nil {
    log.Println(
      "Could not persist: ",
      e.sessionID,
      " ",
      e.accessKey,
      " ",
      e.category,
      " ",
      e.size,
    )
  }
}
