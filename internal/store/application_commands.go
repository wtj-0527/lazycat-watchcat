package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

type ApplicationCommand struct {
	protocol.ApplicationCommand
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	ObservedStatus string    `json:"instanceStatus,omitempty"`
	RequestedAt    time.Time `json:"requestedAt"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	CompletedAt    time.Time `json:"completedAt,omitempty"`
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return 1
	}
	return 0
}

func scanNullableBool(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Int64 != 0
	return &result
}

func (s *Store) CreateApplicationCommand(ctx context.Context, command protocol.ApplicationCommand) (ApplicationCommand, error) {
	command.ID = uuid.NewString()
	command.DeviceID, command.DeployID, command.AppID = strings.TrimSpace(command.DeviceID), strings.TrimSpace(command.DeployID), strings.TrimSpace(command.AppID)
	if command.DeviceID == "" || command.DeployID == "" || command.AppID == "" {
		return ApplicationCommand{}, errors.New("device, deploy and app are required")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_commands(
		id,device_id,deploy_id,app_id,user_id,action,autostart,status,requested_at
	) VALUES(?,?,?,?,?,?,?,?,?)`, command.ID, command.DeviceID, command.DeployID, command.AppID,
		command.UserID, command.Action, nullableBool(command.Autostart), "pending", now.Format(time.RFC3339Nano))
	if err != nil {
		return ApplicationCommand{}, err
	}
	return ApplicationCommand{ApplicationCommand: command, Status: "pending", RequestedAt: now}, nil
}

func (s *Store) ClaimApplicationCommand(ctx context.Context, deviceID string) (*protocol.ApplicationCommand, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, _ = tx.ExecContext(ctx, `UPDATE application_commands SET status='pending',started_at=NULL
		WHERE device_id=? AND status='running' AND started_at<?`, deviceID, time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339Nano))
	var command protocol.ApplicationCommand
	var autostart sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,device_id,deploy_id,app_id,user_id,action,autostart
		FROM application_commands WHERE device_id=? AND status='pending' ORDER BY requested_at LIMIT 1`, deviceID).
		Scan(&command.ID, &command.DeviceID, &command.DeployID, &command.AppID, &command.UserID, &command.Action, &autostart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	command.Autostart = scanNullableBool(autostart)
	result, err := tx.ExecContext(ctx, `UPDATE application_commands SET status='running',started_at=? WHERE id=? AND status='pending'`,
		time.Now().UTC().Format(time.RFC3339Nano), command.ID)
	if err != nil {
		return nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &command, nil
}

func (s *Store) CompleteApplicationCommand(ctx context.Context, deviceID string, result protocol.ApplicationCommandResult) error {
	status := "failed"
	if result.Success {
		status = "succeeded"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deployID, action string
	var requestedAutostart sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT deploy_id,action,autostart FROM application_commands WHERE id=? AND device_id=?`,
		result.ID, deviceID).Scan(&deployID, &action, &requestedAutostart); err != nil {
		return err
	}
	completed := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE application_commands SET status=?,error=?,observed_status=?,completed_at=? WHERE id=? AND device_id=?`,
		status, result.Error, result.InstanceStatus, completed, result.ID, deviceID); err != nil {
		return err
	}
	if result.Success && result.InstanceStatus != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE application_runtime_state SET instance_status=?,updated_at=? WHERE device_id=? AND deploy_id=?`,
			result.InstanceStatus, completed, deviceID, deployID); err != nil {
			return err
		}
	}
	if result.Success && action == "set_autostart" {
		value := result.Autostart
		if value == nil {
			value = scanNullableBool(requestedAutostart)
		}
		if value != nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO application_instance_preferences(device_id,deploy_id,autostart,updated_at)
				VALUES(?,?,?,?) ON CONFLICT(device_id,deploy_id) DO UPDATE SET autostart=excluded.autostart,updated_at=excluded.updated_at`,
				deviceID, deployID, nullableBool(value), completed); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ApplicationCommandByID(ctx context.Context, id string) (ApplicationCommand, error) {
	var command ApplicationCommand
	var autostart sql.NullInt64
	var requested, started, completed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,device_id,deploy_id,app_id,user_id,action,autostart,status,error,observed_status,requested_at,started_at,completed_at
		FROM application_commands WHERE id=?`, id).Scan(
		&command.ID, &command.DeviceID, &command.DeployID, &command.AppID, &command.UserID, &command.Action,
		&autostart, &command.Status, &command.Error, &command.ObservedStatus, &requested, &started, &completed)
	if err != nil {
		return ApplicationCommand{}, err
	}
	command.Autostart = scanNullableBool(autostart)
	command.RequestedAt = parseTime(requested.String)
	command.StartedAt = parseTime(started.String)
	command.CompletedAt = parseTime(completed.String)
	return command, nil
}

func (s *Store) SetApplicationAutostart(ctx context.Context, deviceID, deployID string, value bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_instance_preferences(device_id,deploy_id,autostart,updated_at)
		VALUES(?,?,?,?) ON CONFLICT(device_id,deploy_id) DO UPDATE SET autostart=excluded.autostart,updated_at=excluded.updated_at`,
		deviceID, deployID, nullableBool(&value), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ApplicationAutostartMap(ctx context.Context) (map[string]*bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,deploy_id,autostart FROM application_instance_preferences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*bool{}
	for rows.Next() {
		var deviceID, deployID string
		var value sql.NullInt64
		if err := rows.Scan(&deviceID, &deployID, &value); err != nil {
			return nil, err
		}
		out[deviceID+"\x00"+deployID] = scanNullableBool(value)
	}
	return out, rows.Err()
}
