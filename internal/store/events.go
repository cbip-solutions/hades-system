// SPDX-License-Identifier: MIT
package store

type EventRow struct {
	ID          int64
	TS          int64
	Project     string
	SessionID   string
	SwarmID     string
	TaskID      string
	Type        string
	PayloadJSON string
}

type EventQuery struct {
	Project   string
	SessionID string
	SwarmID   string
	TaskID    string
	Type      string

	SinceTS int64
	UntilTS int64

	Limit int

	Offset int
}

func (s *Store) InsertEvent(ev EventRow) (int64, error) {
	if ev.TS == 0 {
		ev.TS = nowUnix()
	}
	res, err := s.db.Exec(
		`INSERT INTO events (ts, project, session_id, swarm_id, task_id, type, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.TS, ev.Project, ev.SessionID, ev.SwarmID, ev.TaskID, ev.Type, ev.PayloadJSON,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) InsertEventsBatch(rows []EventRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO events (ts, project, session_id, swarm_id, task_id, type, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := nowUnix()
	for _, ev := range rows {
		if ev.TS == 0 {
			ev.TS = now
		}
		if _, err := stmt.Exec(
			ev.TS, ev.Project, ev.SessionID, ev.SwarmID, ev.TaskID, ev.Type, ev.PayloadJSON,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *Store) ListEvents(q EventQuery) ([]EventRow, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}

	query := `SELECT id, ts, project, session_id, swarm_id, task_id, type, payload_json FROM events WHERE 1=1`
	args := []any{}
	if q.Project != "" {
		query += ` AND project = ?`
		args = append(args, q.Project)
	}
	if q.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, q.SessionID)
	}
	if q.SwarmID != "" {
		query += ` AND swarm_id = ?`
		args = append(args, q.SwarmID)
	}
	if q.TaskID != "" {
		query += ` AND task_id = ?`
		args = append(args, q.TaskID)
	}
	if q.Type != "" {
		query += ` AND type = ?`
		args = append(args, q.Type)
	}
	if q.SinceTS > 0 {
		query += ` AND ts >= ?`
		args = append(args, q.SinceTS)
	}
	if q.UntilTS > 0 {
		query += ` AND ts <= ?`
		args = append(args, q.UntilTS)
	}
	query += ` ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EventRow
	for rows.Next() {
		var ev EventRow
		var sessionID, swarmID, taskID, payloadJSON, project sqlNullString
		if err := rows.Scan(
			&ev.ID, &ev.TS, &project, &sessionID, &swarmID, &taskID, &ev.Type, &payloadJSON,
		); err != nil {
			return nil, err
		}
		ev.Project = project.Get()
		ev.SessionID = sessionID.Get()
		ev.SwarmID = swarmID.Get()
		ev.TaskID = taskID.Get()
		ev.PayloadJSON = payloadJSON.Get()
		out = append(out, ev)
	}
	return out, rows.Err()
}
