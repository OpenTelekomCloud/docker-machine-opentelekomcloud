package services

import (
	"testing"

	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/huaweicloud/golangsdk"
	"github.com/huaweicloud/golangsdk/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultAZ     = "eu-de-03"
	defaultFlavor = "s2.large.2"
	defaultImage  = "Standard_Debian_10_latest"
)

var (
	kpName     = RandomString(12, "kp-")
	serverName = RandomString(16, "mahine-")
)

func deleteSubnet(t *testing.T, vpcID string, subnetID string) {
	c := computeClient(t)
	initNetwork(t, c)
	err := c.DeleteSubnet(vpcID, subnetID)
	if err != nil {
		log.Error(err)
		return
	}
	err = c.WaitForSubnetStatus(subnetID, "")
	assert.IsType(t, golangsdk.ErrDefault404{}, err)
}

func deleteVPC(t *testing.T, vpcID string) {
	c := computeClient(t)
	initNetwork(t, c)
	err := c.DeleteVPC(vpcID)
	if err != nil {
		log.Error(err)
		return
	}
	err = c.WaitForVPCStatus(vpcID, "")
	assert.IsType(t, golangsdk.ErrDefault404{}, err)
}

func cleanupResources(t *testing.T) {
	c := computeClient(t)
	initNetwork(t, c)
	srvID, _ := c.FindInstance(serverName)
	if srvID != "" {
		err := c.DeleteInstance(srvID)
		require.NoError(t, err)
		err = c.WaitForInstanceStatus(srvID, "")
		require.IsType(t, golangsdk.ErrDefault404{}, err)
	}
	go func() {
		err := c.DeleteKeyPair(kpName)
		if err != nil {
			log.Error(err)
		}
	}()
	sg, _ := c.FindSecurityGroups([]string{sgName})
	for _, sgID := range sg {
		assert.NoError(t, c.DeleteSecurityGroup(sgID))
	}
	vpcID, _ := c.FindVPC(vpcName)
	if vpcID == "" {
		return
	}
	subnetID, _ := c.FindSubnet(vpcID, subnetName)
	if subnetID != "" {
		deleteSubnet(t, vpcID, subnetID)
	}
	deleteVPC(t, vpcID)

}

func computeClient(t *testing.T) *Client {
	client := authClient(t)
	require.NoError(t, client.InitCompute())
	return client
}

func generatePair(t *testing.T) *ssh.KeyPair {
	pair, err := ssh.NewKeyPair()
	require.NoError(t, err)
	require.NotEmpty(t, pair.PublicKey)
	require.NotEmpty(t, pair.PrivateKey)
	return pair
}

func TestClient_CreateSecurityGroup(t *testing.T) {
	cleanupResources(t)

	client := computeClient(t)
	sg, err := client.CreateSecurityGroup(sgName, PortRange{From: 22})
	require.NoError(t, err)

	sgIDs, err := client.FindSecurityGroups([]string{sgName})
	assert.NoError(t, err)
	assert.EqualValuesf(t, sg.ID, sgIDs[0], invalidFind, "sec group")

	assert.NoError(t, client.DeleteSecurityGroup(sg.ID))
}

func TestClient_CreateKeyPair(t *testing.T) {
	client := computeClient(t)

	_ = client.DeleteKeyPair(kpName) // cleanup

	pair := generatePair(t)
	kp, err := client.CreateKeyPair(kpName, string(pair.PublicKey))
	require.NoError(t, err)
	assert.Empty(t, kp.PrivateKey)

	found, err := client.FindKeyPair(kpName)
	assert.NoError(t, err)
	assert.NotEmpty(t, found)

	err = client.DeleteKeyPair(kpName)
	assert.NoError(t, err)

	found, err = client.FindKeyPair(kpName)
	assert.NoError(t, err)
	assert.Empty(t, found)
}

func TestClient_CreateFloatingIP(t *testing.T) {
	client := computeClient(t)
	ip, err := client.CreateFloatingIP()
	require.NoError(t, err)
	assert.NotEmpty(t, ip)

	addrID, err := client.FindFloatingIP(ip)
	assert.NoError(t, err)
	assert.NotEmpty(t, addrID)

	err = client.DeleteFloatingIP(ip)
	assert.NoError(t, err)

	addrID, err = client.FindFloatingIP(ip)
	assert.NoError(t, err)
	assert.Empty(t, addrID)
}

func (c *Client) waitForInstanceIPBind(instanceID string, ip string, bind bool) error {
	return golangsdk.WaitFor(300, func() (b bool, err error) {
		assigned, err := c.InstanceBindToIP(instanceID, ip)
		if err != nil {
			return true, err
		}
		if assigned == bind {
			return true, nil
		}
		return false, nil
	})
}

// Test whole instance + floating IP workflow
func TestClient_CreateInstance(t *testing.T) {
	cleanupResources(t)

	client := computeClient(t)
	initNetwork(t, client)

	vpc, err := client.CreateVPC(vpcName)
	require.NoError(t, err)
	defer deleteVPC(t, vpc.ID)

	subnet, err := client.CreateSubnet(vpc.ID, subnetName)
	require.NoError(t, err)
	defer deleteSubnet(t, vpc.ID, subnet.ID)

	ip, err := client.CreateFloatingIP()
	require.NoError(t, err)
	defer func() { _ = client.DeleteFloatingIP(ip) }()

	sg, err := client.CreateSecurityGroup(sgName, PortRange{From: 22})
	require.NoError(t, err)
	defer func() { _ = client.DeleteSecurityGroup(sg.ID) }()

	kp, err := client.CreateKeyPair(kpName, "")
	require.NoError(t, err)
	defer func() { _ = client.DeleteKeyPair(kpName) }()

	imgRef, err := client.FindImage(defaultImage)
	require.NoError(t, err)

	opts := &servers.CreateOpts{
		Name:             serverName,
		FlavorName:       defaultFlavor,
		AvailabilityZone: defaultAZ,
		Networks:         []servers.Network{{UUID: subnet.ID}},
	}
	dOpts := &DiskOpts{SourceID: imgRef, Size: 10, Type: "SATA"}
	instance, err := client.CreateInstance(opts, subnet.ID, kp.Name, dOpts)
	require.NoError(t, err)
	assert.NoError(t, client.WaitForInstanceStatus(instance.ID, InstanceStatusRunning))
	defer func() {
		assert.NoError(t, client.DeleteInstance(instance.ID))
		err = client.WaitForInstanceStatus(instance.ID, "")
		require.IsType(t, golangsdk.ErrDefault404{}, err)
	}()

	details, err := client.GetInstanceStatus(instance.ID)
	assert.NoError(t, err)
	if details != nil {
		assert.Equal(t, details.Name, serverName)
	}

	assert.NoError(t, client.BindFloatingIP(ip, instance.ID))
	assert.NoError(t, err)
	err = client.waitForInstanceIPBind(instance.ID, ip, true)

	assert.NoError(t, client.UnbindFloatingIP(ip, instance.ID))
	details, _ = client.GetInstanceStatus(instance.ID)
	assert.NotNil(t, details)
	err = client.waitForInstanceIPBind(instance.ID, ip, false)

	assert.NoError(t, client.StopInstance(instance.ID))
	assert.NoError(t, client.WaitForInstanceStatus(instance.ID, InstanceStatusStopped))

	assert.NoError(t, client.StartInstance(instance.ID))
	assert.NoError(t, client.WaitForInstanceStatus(instance.ID, InstanceStatusRunning))

	assert.NoError(t, client.RestartInstance(instance.ID))
	assert.NoError(t, client.WaitForInstanceStatus(instance.ID, InstanceStatusRunning))

}

func TestClient_FindFlavor(t *testing.T) {
	client := computeClient(t)
	flvID, err := client.FindFlavor(defaultFlavor)
	require.NoError(t, err)
	require.NotEmpty(t, flvID)
}

func TestClient_FindImage(t *testing.T) {
	client := computeClient(t)
	imgID, err := client.FindImage(defaultImage)
	require.NoError(t, err)
	require.NotEmpty(t, imgID)
}
