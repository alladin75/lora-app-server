package storage

import (
	"fmt"
	"time"

	uuid "github.com/gofrs/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/lorawan"
)

// FUOTADeploymentState defines the fuota deployment state.
type FUOTADeploymentState string

// FUOTA deployment states.
const (
	FUOTADeploymentMulticastSetup         FUOTADeploymentState = "MC_SETUP"
	FUOTADeploymentFragmentationSessSetup FUOTADeploymentState = "FRAG_SESS_SETUP"
	FUOTADeploymentMulticastSessCSetup    FUOTADeploymentState = "MC_SESS_C_SETUP"
	FUOTADeploymentEnqueue                FUOTADeploymentState = "ENQUEUE"
	FUOTADeploymentWaitingTx              FUOTADeploymentState = "WAITING_TX"
	FUOTADeploymentTransmitted            FUOTADeploymentState = "TRANSMITTED"
	FUOTADeploymentStatusRequested        FUOTADeploymentState = "STATUS_REQUESTED"
	FUOTADeploymentDone                   FUOTADeploymentState = "DONE"
)

// FUOTADeploymentDeviceState defines the fuota deployment device state.
type FUOTADeploymentDeviceState string

// FUOTA deployment device states.
const (
	FUOTADeploymentDevicePending FUOTADeploymentDeviceState = "PENDING"
	FUOTADeploymentDeviceSuccess FUOTADeploymentDeviceState = "SUCCESS"
	FUOTADeploymentDeviceError   FUOTADeploymentDeviceState = "ERROR"
)

// FUOTADeployment defiles a firmware update over the air deployment.
type FUOTADeployment struct {
	ID                  uuid.UUID            `db:"id"`
	CreatedAt           time.Time            `db:"created_at"`
	UpdatedAt           time.Time            `db:"updated_at"`
	Name                string               `db:"name"`
	MulticastGroupID    *uuid.UUID           `db:"multicast_group_id"`
	FragmentationMatrix uint8                `db:"fragmentation_matrix"`
	Descriptor          [4]byte              `db:"descriptor"`
	Payload             []byte               `db:"payload"`
	FragSize            int                  `db:"frag_size"`
	Redundancy          int                  `db:"redundancy"`
	BlockAckDelay       int                  `db:"block_ack_delay"`
	MulticastTimeout    int                  `db:"multicast_timeout"`
	State               FUOTADeploymentState `db:"state"`
	UnicastTimeout      time.Duration        `db:"unicast_timeout"`
	NextStepAfter       time.Time            `db:"next_step_after"`
}

// FUOTADeploymentDeviceListItem defines the Device as FUOTA deployment list item.
type FUOTADeploymentDeviceListItem struct {
	CreatedAt         time.Time                  `db:"created_at"`
	UpdatedAt         time.Time                  `db:"updated_at"`
	FUOTADeploymentID uuid.UUID                  `db:"fuota_deployment_id"`
	DevEUI            lorawan.EUI64              `db:"dev_eui"`
	DeviceName        string                     `db:"device_name"`
	State             FUOTADeploymentDeviceState `db:"state"`
	ErrorMessage      string                     `db:"error_message"`
}

// CreateFUOTADeploymentForDevice creates and initializes a FUOTA deployment
// for the given device.
func CreateFUOTADeploymentForDevice(db sqlx.Ext, fd *FUOTADeployment, devEUI lorawan.EUI64) error {
	now := time.Now()
	var err error
	fd.ID, err = uuid.NewV4()
	if err != nil {
		return errors.Wrap(err, "new uuid error")
	}

	fd.CreatedAt = now
	fd.UpdatedAt = now
	fd.NextStepAfter = now
	if fd.State == "" {
		fd.State = FUOTADeploymentMulticastSetup
	}

	_, err = db.Exec(`
		insert into fuota_deployment (
			id,
			created_at,
			updated_at,
			name,
			multicast_group_id,
			fragmentation_matrix,
			descriptor,
			payload,
			state,
			next_step_after,
			unicast_timeout,
			frag_size,
			redundancy,
			block_ack_delay,
			multicast_timeout
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		fd.ID,
		fd.CreatedAt,
		fd.UpdatedAt,
		fd.Name,
		fd.MulticastGroupID,
		[]byte{fd.FragmentationMatrix},
		fd.Descriptor[:],
		fd.Payload,
		fd.State,
		fd.NextStepAfter,
		fd.UnicastTimeout,
		fd.FragSize,
		fd.Redundancy,
		fd.BlockAckDelay,
		fd.MulticastTimeout,
	)
	if err != nil {
		return handlePSQLError(Insert, err, "insert error")
	}

	_, err = db.Exec(`
		insert into fuota_deployment_device (
			fuota_deployment_id,
			dev_eui,
			created_at,
			updated_at,
			state,
			error_message
		) values ($1, $2, $3, $4, $5, $6)`,
		fd.ID,
		devEUI,
		now,
		now,
		FUOTADeploymentDevicePending,
		"",
	)
	if err != nil {
		return handlePSQLError(Insert, err, "insert error")
	}

	log.WithFields(log.Fields{
		"dev_eui": devEUI,
		"id":      fd.ID,
	}).Info("fuota deploymented created for device")

	return nil
}

// GetFUOTADeployment returns the FUOTA deployment for the given ID.
func GetFUOTADeployment(db sqlx.Ext, id uuid.UUID, forUpdate bool) (FUOTADeployment, error) {
	var fu string
	if forUpdate {
		fu = " for update"
	}

	row := db.QueryRowx(`
		select
			id,
			created_at,
			updated_at,
			name,
			multicast_group_id,
			fragmentation_matrix,
			descriptor,
			payload,
			state,
			next_step_after,
			unicast_timeout,
			frag_size,
			redundancy,
			block_ack_delay,
			multicast_timeout
		from
			fuota_deployment
		where
			id = $1`+fu,
		id,
	)

	return scanFUOTADeployment(row)
}

// GetPendingFUOTADeployments returns the pending FUOTA deployments.
func GetPendingFUOTADeployments(db sqlx.Ext, batchSize int) ([]FUOTADeployment, error) {
	var out []FUOTADeployment

	rows, err := db.Queryx(`
		select
			id,
			created_at,
			updated_at,
			name,
			multicast_group_id,
			fragmentation_matrix,
			descriptor,
			payload,
			state,
			next_step_after,
			unicast_timeout,
			frag_size,
			redundancy,
			block_ack_delay,
			multicast_timeout
		from
			fuota_deployment
		where
			state != $1
			and next_step_after <= $2
			and multicast_group_id IS NOT NULL
		limit $3
		for update
		skip locked`,
		FUOTADeploymentDone,
		time.Now(),
		batchSize,
	)
	if err != nil {
		return nil, handlePSQLError(Select, err, "select error")
	}
	defer rows.Close()

	for rows.Next() {
		item, err := scanFUOTADeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}

	return out, nil
}

// UpdateFUOTADeployment updates the given FUOTA deployment.
func UpdateFUOTADeployment(db sqlx.Ext, fd *FUOTADeployment) error {
	fd.UpdatedAt = time.Now()

	res, err := db.Exec(`
		update fuota_deployment
		set
			updated_at = $2,
			name = $3,
			multicast_group_id = $4,
			fragmentation_matrix = $5,
			descriptor = $6,
			payload = $7,
			state = $8,
			next_step_after = $9,
			unicast_timeout = $10,
			frag_size = $11,
			redundancy = $12,
			block_ack_delay = $13,
			multicast_timeout = $14
		where
			id = $1`,
		fd.ID,
		fd.UpdatedAt,
		fd.Name,
		fd.MulticastGroupID,
		[]byte{fd.FragmentationMatrix},
		fd.Descriptor[:],
		fd.Payload,
		fd.State,
		fd.NextStepAfter,
		fd.UnicastTimeout,
		fd.FragSize,
		fd.Redundancy,
		fd.BlockAckDelay,
		fd.MulticastTimeout,
	)
	if err != nil {
		return handlePSQLError(Update, err, "update error")
	}
	ra, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "get rows affected error")
	}
	if ra == 0 {
		return ErrDoesNotExist
	}

	log.WithFields(log.Fields{
		"id":    fd.ID,
		"state": fd.State,
	}).Info("fuota deployment updated")

	return nil
}

// GetFUOTADeploymentDeviceCount returns the device count for the given
// FUOTA deployment ID.
func GetFUOTADeploymentDeviceCount(db sqlx.Queryer, fuotaDeploymentID uuid.UUID) (int, error) {
	var count int
	err := sqlx.Get(db, &count, `
		select
			count(*)
		from
			fuota_deployment_device
		where
			fuota_deployment_id = $1`,
		fuotaDeploymentID,
	)
	if err != nil {
		return 0, handlePSQLError(Select, err, "select error")
	}

	return count, nil
}

// GetFUOTADeploymentDevices returns a slice of devices for the given FUOTA
// deployment ID.
func GetFUOTADeploymentDevices(db sqlx.Queryer, fuotaDeploymentID uuid.UUID, limit, offset int) ([]FUOTADeploymentDeviceListItem, error) {
	var out []FUOTADeploymentDeviceListItem

	err := sqlx.Select(db, &out, `
		select
			dd.created_at,
			dd.updated_at,
			dd.fuota_deployment_id,
			dd.dev_eui,
			d.name as device_name,
			dd.state,
			dd.error_message
		from
			fuota_deployment_device dd
		inner join
			device d
			on dd.dev_eui = d.dev_eui
		where
			dd.fuota_deployment_id = $3
		order by
			d.Name
		limit $1
		offset $2`,
		limit,
		offset,
		fuotaDeploymentID,
	)
	if err != nil {
		return nil, handlePSQLError(Select, err, "select error")
	}

	return out, nil
}

func scanFUOTADeployment(row sqlx.ColScanner) (FUOTADeployment, error) {
	var fd FUOTADeployment

	var fragmentationMatrix []byte
	var descriptor []byte

	err := row.Scan(
		&fd.ID,
		&fd.CreatedAt,
		&fd.UpdatedAt,
		&fd.Name,
		&fd.MulticastGroupID,
		&fragmentationMatrix,
		&descriptor,
		&fd.Payload,
		&fd.State,
		&fd.NextStepAfter,
		&fd.UnicastTimeout,
		&fd.FragSize,
		&fd.Redundancy,
		&fd.BlockAckDelay,
		&fd.MulticastTimeout,
	)
	if err != nil {
		return fd, handlePSQLError(Select, err, "select error")
	}

	if len(fragmentationMatrix) != 1 {
		return fd, fmt.Errorf("FragmentationMatrix must have length 1, got: %d", len(fragmentationMatrix))
	}
	fd.FragmentationMatrix = fragmentationMatrix[0]

	if len(descriptor) != len(fd.Descriptor) {
		return fd, fmt.Errorf("Descriptor must have length: %d, got: %d", len(fd.Descriptor), len(descriptor))
	}
	copy(fd.Descriptor[:], descriptor)

	return fd, nil
}
