package storage

import (
	"testing"
	"time"

	uuid "github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	"github.com/brocaar/lora-app-server/internal/backend/networkserver"
	nsmock "github.com/brocaar/lora-app-server/internal/backend/networkserver/mock"
	"github.com/brocaar/lorawan"
)

func (ts *StorageTestSuite) TestFUOTADeployment() {
	assert := require.New(ts.T())

	nsClient := nsmock.NewClient()
	networkserver.SetPool(nsmock.NewPool(nsClient))

	n := NetworkServer{
		Name:   "test",
		Server: "test:1234",
	}
	assert.NoError(CreateNetworkServer(ts.tx, &n))

	org := Organization{
		Name: "test-org",
	}
	assert.NoError(CreateOrganization(ts.tx, &org))

	sp := ServiceProfile{
		Name:            "test-sp",
		OrganizationID:  org.ID,
		NetworkServerID: n.ID,
	}
	assert.NoError(CreateServiceProfile(ts.tx, &sp))
	var spID uuid.UUID
	copy(spID[:], sp.ServiceProfile.Id)

	app := Application{
		Name:             "test-app",
		OrganizationID:   org.ID,
		ServiceProfileID: spID,
	}
	assert.NoError(CreateApplication(ts.tx, &app))

	dp := DeviceProfile{
		Name:            "test-dp",
		OrganizationID:  org.ID,
		NetworkServerID: n.ID,
	}
	assert.NoError(CreateDeviceProfile(ts.tx, &dp))
	var dpID uuid.UUID
	copy(dpID[:], dp.DeviceProfile.Id)

	d := Device{
		DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
		ApplicationID:   app.ID,
		DeviceProfileID: dpID,
		Name:            "test-device",
		Description:     "test device",
	}
	assert.NoError(CreateDevice(ts.tx, &d))

	mg := MulticastGroup{
		Name:             "test-mg",
		MCAppSKey:        lorawan.AES128Key{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8},
		MCKey:            lorawan.AES128Key{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8},
		ServiceProfileID: spID,
	}
	assert.NoError(CreateMulticastGroup(ts.tx, &mg))
	var mgID uuid.UUID
	copy(mgID[:], mg.MulticastGroup.Id)

	ts.T().Run("Create fuota deployment for device", func(t *testing.T) {
		assert := require.New(t)

		fd := FUOTADeployment{
			Name:                "test deployment",
			MulticastGroupID:    &mgID,
			FragmentationMatrix: 3,
			Descriptor:          [4]byte{1, 2, 3, 4},
			Payload:             []byte{5, 6, 7, 8},
			UnicastTimeout:      time.Minute,
			FragSize:            10,
			Redundancy:          20,
			BlockAckDelay:       6,
			MulticastTimeout:    3,
		}
		assert.NoError(CreateFUOTADeploymentForDevice(ts.tx, &fd, d.DevEUI))
		fd.CreatedAt = fd.CreatedAt.UTC().Round(time.Millisecond)
		fd.UpdatedAt = fd.UpdatedAt.UTC().Round(time.Millisecond)
		fd.NextStepAfter = fd.NextStepAfter.UTC().Round(time.Millisecond)

		assert.Equal(FUOTADeploymentMulticastSetup, fd.State)

		t.Run("Get fuota deployment", func(t *testing.T) {
			assert := require.New(t)

			fdGet, err := GetFUOTADeployment(ts.tx, fd.ID, false)
			assert.NoError(err)
			fdGet.CreatedAt = fdGet.CreatedAt.UTC().Round(time.Millisecond)
			fdGet.UpdatedAt = fdGet.UpdatedAt.UTC().Round(time.Millisecond)
			fdGet.NextStepAfter = fdGet.NextStepAfter.UTC().Round(time.Millisecond)
			assert.Equal(fd, fdGet)
		})

		t.Run("Get pending fuota deployments", func(t *testing.T) {
			assert := require.New(t)

			pending, err := GetPendingFUOTADeployments(ts.tx, 10)
			assert.NoError(err)
			assert.Len(pending, 1)
		})

		t.Run("Get fuota deployment device count", func(t *testing.T) {
			assert := require.New(t)

			count, err := GetFUOTADeploymentDeviceCount(ts.tx, fd.ID)
			assert.NoError(err)
			assert.Equal(1, count)
		})

		t.Run("Get fuota deployment devices", func(t *testing.T) {
			assert := require.New(t)

			devices, err := GetFUOTADeploymentDevices(ts.tx, fd.ID, 10, 0)
			assert.NoError(err)
			assert.Len(devices, 1)

			assert.Equal(fd.ID, devices[0].FUOTADeploymentID)
			assert.Equal(d.DevEUI, devices[0].DevEUI)
			assert.Equal(d.Name, devices[0].DeviceName)
			assert.Equal(FUOTADeploymentDevicePending, devices[0].State)
			assert.Equal("", devices[0].ErrorMessage)
		})

		t.Run("Update fuota deployment + set done", func(t *testing.T) {
			assert := require.New(t)

			fd.Name = "updated deployment"
			fd.FragmentationMatrix = 2
			fd.Descriptor = [4]byte{4, 3, 2, 1}
			fd.Payload = []byte{1, 2, 1, 2}
			fd.State = FUOTADeploymentDone
			fd.NextStepAfter = time.Now()
			fd.UnicastTimeout = time.Minute * 2
			fd.FragSize = 20
			fd.Redundancy = 30
			fd.BlockAckDelay = 7
			fd.MulticastTimeout = 4

			assert.NoError(UpdateFUOTADeployment(ts.tx, &fd))
			fd.UpdatedAt = fd.UpdatedAt.UTC().Round(time.Millisecond)
			fd.NextStepAfter = fd.NextStepAfter.UTC().Round(time.Millisecond)

			fdGet, err := GetFUOTADeployment(ts.tx, fd.ID, false)
			assert.NoError(err)
			fdGet.CreatedAt = fdGet.CreatedAt.UTC().Round(time.Millisecond)
			fdGet.UpdatedAt = fdGet.UpdatedAt.UTC().Round(time.Millisecond)
			fdGet.NextStepAfter = fdGet.NextStepAfter.UTC().Round(time.Millisecond)

			assert.Equal(fd, fdGet)

			t.Run("Get pending fuota deployments", func(t *testing.T) {
				assert := require.New(t)

				pending, err := GetPendingFUOTADeployments(ts.tx, 10)
				assert.NoError(err)
				assert.Len(pending, 0)
			})
		})
	})
}
