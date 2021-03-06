package nfsbroker_test

import (
	"bytes"
	"errors"

	"code.cloudfoundry.org/lager/lagertest"
	"github.com/pivotal-cf/brokerapi"

	"context"

	"encoding/json"

	"fmt"

	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/nfsbroker/nfsbroker"
	"code.cloudfoundry.org/nfsbroker/nfsbrokerfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Broker", func() {
	var (
		broker    *nfsbroker.Broker
		fakeOs    *os_fake.FakeOs
		logger    lager.Logger
		ctx       context.Context
		fakeStore *nfsbrokerfakes.FakeStore
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test-broker")
		ctx = context.TODO()
		fakeOs = &os_fake.FakeOs{}
		fakeStore = &nfsbrokerfakes.FakeStore{}
	})

	Context("when creating first time", func() {
		BeforeEach(func() {
			source := nfsbroker.NewNfsBrokerConfigDetails()
			source.ReadConf("uid,gid", "", []string{"uid", "gid"})
			mounts := nfsbroker.NewNfsBrokerConfigDetails()
			mounts.ReadConf("sloppy_mount,allow_other,allow_root,multithread,default_permissions,fusenfs_uid,fusenfs_gid", "sloppy_mount:true", []string{})
			broker = nfsbroker.New(
				logger,
				"service-name", "service-id", "/fake-dir",
				fakeOs,
				nil,
				fakeStore,
				nfsbroker.NewNfsBrokerConfig(source, mounts),
			)
		})

		Context(".Services", func() {
			It("returns the service catalog as appropriate", func() {
				result := broker.Services(ctx)[0]
				Expect(result.ID).To(Equal("service-id"))
				Expect(result.Name).To(Equal("service-name"))
				//Expect(result.Description).To(Equal("Existing NFSv3 volumes (see: https://code.cloudfoundry.org/nfs-volume-release/)"))
				Expect(result.Description).To(Equal("Please contact the support team (support.fap@orange.com), if you need help to use this service (NFSv3 volumes)."))
				Expect(result.Bindable).To(Equal(true))
				Expect(result.PlanUpdatable).To(Equal(false))
				Expect(result.Tags).To(ContainElement("nfs"))
				Expect(result.Requires).To(ContainElement(brokerapi.RequiredPermission("volume_mount")))

				Expect(result.Plans[0].Name).To(Equal("Existing"))
				Expect(result.Plans[0].ID).To(Equal("Existing"))
				//Expect(result.Plans[0].Description).To(Equal("A preexisting filesystem"))
				Expect(result.Plans[0].Description).To(Equal("This service has to be instantiated in command line with specific parameters."))
			})
		})

		Context(".Provision", func() {
			var (
				instanceID       string
				provisionDetails brokerapi.ProvisionDetails
				asyncAllowed     bool

				spec brokerapi.ProvisionedServiceSpec
				err  error
			)

			BeforeEach(func() {
				instanceID = "some-instance-id"

				configuration := map[string]interface{}{"share": "server:/some-share"}
				buf := &bytes.Buffer{}
				_ = json.NewEncoder(buf).Encode(configuration)
				provisionDetails = brokerapi.ProvisionDetails{PlanID: "Existing", RawParameters: json.RawMessage(buf.Bytes())}
				asyncAllowed = false
				fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{}, errors.New("not found"))
			})

			JustBeforeEach(func() {
				spec, err = broker.Provision(ctx, instanceID, provisionDetails, asyncAllowed)
			})

			It("should not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should provision the service instance synchronously", func() {
				Expect(spec.IsAsync).To(Equal(false))
			})

			It("should write state", func() {
				Expect(fakeStore.SaveCallCount()).Should(BeNumerically(">", 0))
			})

			Context("create-service was given invalid JSON", func() {
				BeforeEach(func() {
					badJson := []byte("{this is not json")
					provisionDetails = brokerapi.ProvisionDetails{PlanID: "Existing", RawParameters: json.RawMessage(badJson)}
				})

				It("errors", func() {
					Expect(err).To(Equal(brokerapi.ErrRawParamsInvalid))
				})

			})
			Context("create-service was given valid JSON but no 'share' key", func() {
				BeforeEach(func() {
					configuration := map[string]interface{}{"unknown key": "server:/some-share"}
					buf := &bytes.Buffer{}
					_ = json.NewEncoder(buf).Encode(configuration)
					provisionDetails = brokerapi.ProvisionDetails{PlanID: "Existing", RawParameters: json.RawMessage(buf.Bytes())}
				})

				It("errors", func() {
					Expect(err).To(Equal(errors.New("config requires a \"share\" key")))
				})
			})

			Context("when the service instance already exists with the same details", func() {
				BeforeEach(func() {
					fakeStore.IsInstanceConflictReturns(false)
				})

				It("should not error", func() {
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("when the service instance already exists with different details", func() {
				BeforeEach(func() {
					fakeStore.IsInstanceConflictReturns(true)
				})

				It("should error", func() {
					Expect(err).To(Equal(brokerapi.ErrInstanceAlreadyExists))
				})
			})

			Context("when the service instance creation fails", func() {
				BeforeEach(func() {
					fakeStore.CreateInstanceDetailsReturns(errors.New("badness"))
				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})
			})

			Context("when the save fails", func() {
				BeforeEach(func() {
					fakeStore.SaveReturns(errors.New("badness"))
				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})
			})

		})

		Context(".Deprovision", func() {
			var (
				instanceID       string
				asyncAllowed     bool
				provisionDetails brokerapi.ProvisionDetails

				err error
			)

			BeforeEach(func() {
				instanceID = "some-instance-id"
				provisionDetails = brokerapi.ProvisionDetails{PlanID: "Existing"}
				asyncAllowed = true

			})

			JustBeforeEach(func() {
				_, err = broker.Deprovision(ctx, instanceID, brokerapi.DeprovisionDetails{}, asyncAllowed)
			})

			Context("when the instance does not exist", func() {
				BeforeEach(func() {
					instanceID = "does-not-exist"
					fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{}, brokerapi.ErrInstanceDoesNotExist)
				})

				It("should fail", func() {
					Expect(err).To(Equal(brokerapi.ErrInstanceDoesNotExist))
				})
			})

			Context("given an existing instance", func() {
				var (
					previousSaveCallCount int
				)

				BeforeEach(func() {
					instanceID = "some-instance-id"

					configuration := map[string]interface{}{"share": "server:/some-share"}
					buf := &bytes.Buffer{}
					_ = json.NewEncoder(buf).Encode(configuration)
					provisionDetails = brokerapi.ProvisionDetails{PlanID: "Existing", RawParameters: json.RawMessage(buf.Bytes())}
					asyncAllowed = false
					fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{ServiceID: instanceID}, nil)
					previousSaveCallCount = fakeStore.SaveCallCount()
				})

				It("should succeed", func() {
					Expect(err).NotTo(HaveOccurred())
				})

				It("save state", func() {
					Expect(fakeStore.SaveCallCount()).To(Equal(previousSaveCallCount + 1))
				})

				Context("when deletion of the instance fails", func() {
					BeforeEach(func() {
						fakeStore.DeleteInstanceDetailsReturns(errors.New("badness"))
					})

					It("should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
			})

			Context("when the save fails", func() {
				BeforeEach(func() {
					fakeStore.SaveReturns(errors.New("badness"))
				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})
			})
		})

		Context(".LastOperation", func() {
			It("errors", func() {
				_, err := broker.LastOperation(ctx, "non-existant", "provision")
				Expect(err).To(HaveOccurred())
			})
		})

		Context(".Bind", func() {
			var (
				instanceID  string
				bindDetails brokerapi.BindDetails

				uid, gid string
			)

			BeforeEach(func() {
				instanceID = "some-instance-id"
				uid = "1234"
				gid = "5678"

				fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{ServiceID: instanceID, Share: "server:/some-share"}, nil)
				fakeStore.RetrieveBindingDetailsReturns(brokerapi.BindDetails{}, errors.New("yar"))

				bindDetails = brokerapi.BindDetails{AppGUID: "guid", Parameters: map[string]interface{}{
					nfsbroker.Username: "principal name",
					nfsbroker.Secret:   "some keytab data",
					"uid":              uid,
					"gid":              gid,
				},
				}
			})

			It("passes `share` from create-service into `mountConfig.ip` on the bind response", func() {
				binding, err := broker.Bind(ctx, instanceID, "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())

				mc := binding.VolumeMounts[0].Device.MountConfig
				share, ok := mc["source"].(string)

				Expect(ok).To(BeTrue())
				Expect(share).To(ContainSubstring("nfs://server:/some-share?"))
				Expect(share).To(ContainSubstring(fmt.Sprintf("uid=%s", uid)))
				Expect(share).To(ContainSubstring(fmt.Sprintf("gid=%s", gid)))
			})

			It("includes empty credentials to prevent CAPI crash", func() {
				binding, err := broker.Bind(ctx, instanceID, "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())

				Expect(binding.Credentials).NotTo(BeNil())
			})

			It("uses the instance id in the default container path", func() {
				binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())
				Expect(binding.VolumeMounts[0].ContainerDir).To(Equal("/var/vcap/data/some-instance-id"))
			})

			It("flows container path through", func() {
				bindDetails.Parameters["mount"] = "/var/vcap/otherdir/something"
				binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())
				Expect(binding.VolumeMounts[0].ContainerDir).To(Equal("/var/vcap/otherdir/something"))
			})

			It("uses rw as its default mode", func() {
				binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())
				Expect(binding.VolumeMounts[0].Mode).To(Equal("rw"))
			})

			//It("sets mode to `r` when readonly is true", func() {
			//	bindDetails.Parameters["readonly"] = true
			//	binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
			//	Expect(err).NotTo(HaveOccurred())
			//
			//	Expect(binding.VolumeMounts[0].Mode).To(Equal("r"))
			//	Expect(binding.VolumeMounts[0].Device.MountConfig["readonly"]).To(Equal(true))
			//})

			It("should write state", func() {
				previousSaveCallCount := fakeStore.SaveCallCount()
				_, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeStore.SaveCallCount()).To(Equal(previousSaveCallCount + 1))
			})

			It("errors if mode is not a boolean", func() {
				bindDetails.Parameters["readonly"] = ""
				_, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).To(Equal(brokerapi.ErrRawParamsInvalid))
			})

			It("fills in the driver name", func() {
				binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())

				Expect(binding.VolumeMounts[0].Driver).To(Equal("nfsv3driver"))
			})

			It("fills in the volume id", func() {
				binding, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				Expect(err).NotTo(HaveOccurred())

				Expect(binding.VolumeMounts[0].Device.VolumeId).To(ContainSubstring("some-instance-id"))
			})

			Context("when the binding already exists", func() {

				It("doesn't error when binding the same details", func() {
					fakeStore.IsBindingConflictReturns(false)
					_, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
					Expect(err).NotTo(HaveOccurred())
				})

				It("errors when binding different details", func() {
					fakeStore.IsBindingConflictReturns(true)
					_, err := broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
					Expect(err).To(Equal(brokerapi.ErrBindingAlreadyExists))
				})
			})

			Context("given another binding with the same share", func() {
				var (
					err       error
					bindSpec1 brokerapi.Binding
				)

				BeforeEach(func() {
					bindSpec1, err = broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
					Expect(err).NotTo(HaveOccurred())
				})

				Context("given different options", func() {
					var (
						bindSpec2 brokerapi.Binding
					)
					BeforeEach(func() {
						bindDetails.Parameters["uid"] = "3000"
						bindDetails.Parameters["gid"] = "3000"
						bindSpec2, err = broker.Bind(ctx, "some-instance-id", "binding-id-2", bindDetails)
						Expect(err).NotTo(HaveOccurred())
					})

					It("should issue a volume mount with a different volume ID", func() {
						Expect(bindSpec1.VolumeMounts[0].Device.VolumeId).NotTo(Equal(bindSpec2.VolumeMounts[0].Device.VolumeId))
					})
				})
			})

			Context("when the binding cannot be stored", func() {
				var (
					err error
				)

				BeforeEach(func() {
					fakeStore.CreateBindingDetailsReturns(errors.New("badness"))
					_, err = broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)

				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})

			})

			Context("when the save fails", func() {
				var (
					err error
				)
				BeforeEach(func() {
					fakeStore.SaveReturns(errors.New("badness"))
					_, err = broker.Bind(ctx, "some-instance-id", "binding-id", bindDetails)
				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})
			})

			It("errors when the service instance does not exist", func() {
				fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{}, errors.New("Awesome!"))
				_, err := broker.Bind(ctx, "nonexistent-instance-id", "binding-id", brokerapi.BindDetails{AppGUID: "guid"})
				Expect(err).To(Equal(brokerapi.ErrInstanceDoesNotExist))
			})

			It("errors when the app guid is not provided", func() {
				_, err := broker.Bind(ctx, "some-instance-id", "binding-id", brokerapi.BindDetails{})
				Expect(err).To(Equal(brokerapi.ErrAppGuidNotProvided))
			})

			Context("given allowed, default and mandatory parameters are empty", func() {
				BeforeEach(func() {
					source := nfsbroker.NewNfsBrokerConfigDetails()
					source.ReadConf("", "", []string{})
					mounts := nfsbroker.NewNfsBrokerConfigDetails()
					mounts.ReadConf("", "", []string{})
					broker = nfsbroker.New(
						logger,
						"service-name", "service-id", "/fake-dir",
						fakeOs,
						nil,
						fakeStore,
						nfsbroker.NewNfsBrokerConfig(source, mounts),
					)
				})

				Context("given allow_root=true is supplied", func() {
					BeforeEach(func() {
						bindDetails = brokerapi.BindDetails{AppGUID: "guid", Parameters: map[string]interface{}{
							nfsbroker.Username: "principal name",
							nfsbroker.Secret:   "some keytab data",
							"allow_root":       true,
						},
						}
					})

					It("should return with an error", func() {
						_, err := broker.Bind(ctx, instanceID, "binding-id", bindDetails)
						Expect(err).To(HaveOccurred())
					})
				})
			})

			Context("given allowed, default and mandatory parameters are empty, excepted for mount default with sloppy_mount=true is supplied ", func() {
				BeforeEach(func() {
					source := nfsbroker.NewNfsBrokerConfigDetails()
					source.ReadConf("", "", []string{})
					mounts := nfsbroker.NewNfsBrokerConfigDetails()
					mounts.ReadConf("", "sloppy_mount:true", []string{})
					broker = nfsbroker.New(
						logger,
						"service-name", "service-id", "/fake-dir",
						fakeOs,
						nil,
						fakeStore,
						nfsbroker.NewNfsBrokerConfig(source, mounts),
					)
				})

				Context("given allow_root=true is supplied", func() {
					BeforeEach(func() {
						bindDetails = brokerapi.BindDetails{AppGUID: "guid", Parameters: map[string]interface{}{
							nfsbroker.Username: "principal name",
							nfsbroker.Secret:   "some keytab data",
							"allow_root":       true,
						},
						}
					})

					It("do not flows allow_root option through", func() {
						binding, err := broker.Bind(ctx, instanceID, "binding-id", bindDetails)
						Expect(err).NotTo(HaveOccurred())

						mc := binding.VolumeMounts[0].Device.MountConfig
						_, ok := mc["allow_root"]

						Expect(ok).To(BeFalse())
					})
				})
			})

			Context("given default and mandatory parameters are empty, allowed parameters contain allow_root", func() {
				BeforeEach(func() {
					source := nfsbroker.NewNfsBrokerConfigDetails()
					source.ReadConf("", "", []string{})
					mounts := nfsbroker.NewNfsBrokerConfigDetails()
					mounts.ReadConf("allow_root", "", []string{})
					broker = nfsbroker.New(
						logger,
						"service-name", "service-id", "/fake-dir",
						fakeOs,
						nil,
						fakeStore,
						nfsbroker.NewNfsBrokerConfig(source, mounts),
					)
				})

				Context("given allow_root=true is supplied", func() {
					BeforeEach(func() {
						bindDetails = brokerapi.BindDetails{AppGUID: "guid", Parameters: map[string]interface{}{
							nfsbroker.Username: "principal name",
							nfsbroker.Secret:   "some keytab data",
							"allow_root":       true,
						},
						}
					})

					It("flows allow_root=true option through", func() {
						binding, err := broker.Bind(ctx, instanceID, "binding-id", bindDetails)
						Expect(err).NotTo(HaveOccurred())

						mc := binding.VolumeMounts[0].Device.MountConfig
						ar, ok := mc["allow_root"].(string)

						Expect(ok).To(BeTrue())
						Expect(ar).To(Equal("true"))
					})
				})
			})
		})

		Context(".Unbind", func() {
			var (
				instanceID  string
				bindDetails brokerapi.BindDetails
			)

			BeforeEach(func() {
				instanceID = "some-instance-id"
				bindDetails = brokerapi.BindDetails{AppGUID: "guid", Parameters: map[string]interface{}{nfsbroker.Username: "principal name", nfsbroker.Secret: "some keytab data", "uid": "1000", "gid": "1000"}}

				fakeStore.RetrieveBindingDetailsReturns(bindDetails, nil)
			})
			It("unbinds a bound service instance from an app", func() {
				err := broker.Unbind(ctx, "some-instance-id", "binding-id", brokerapi.UnbindDetails{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("fails when trying to unbind a instance that has not been provisioned", func() {
				fakeStore.RetrieveInstanceDetailsReturns(nfsbroker.ServiceInstance{}, errors.New("Shazaam!"))
				err := broker.Unbind(ctx, "some-other-instance-id", "binding-id", brokerapi.UnbindDetails{})
				Expect(err).To(Equal(brokerapi.ErrInstanceDoesNotExist))
			})

			It("fails when trying to unbind a binding that has not been bound", func() {
				fakeStore.RetrieveBindingDetailsReturns(brokerapi.BindDetails{}, errors.New("Hooray!"))
				err := broker.Unbind(ctx, "some-instance-id", "some-other-binding-id", brokerapi.UnbindDetails{})
				Expect(err).To(Equal(brokerapi.ErrBindingDoesNotExist))
			})
			It("should write state", func() {
				previousCallCount := fakeStore.SaveCallCount()
				err := broker.Unbind(ctx, "some-instance-id", "binding-id", brokerapi.UnbindDetails{})
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeStore.SaveCallCount()).To(Equal(previousCallCount + 1))
			})

			Context("when the save fails", func() {
				BeforeEach(func() {
					fakeStore.SaveReturns(errors.New("badness"))
				})

				It("should error", func() {
					err := broker.Unbind(ctx, "some-instance-id", "binding-id", brokerapi.UnbindDetails{})
					Expect(err).To(HaveOccurred())
				})
			})

			Context("when deletion of the binding details fails", func() {
				BeforeEach(func() {
					fakeStore.DeleteBindingDetailsReturns(errors.New("badness"))
				})

				It("should error", func() {
					err := broker.Unbind(ctx, "some-instance-id", "binding-id", brokerapi.UnbindDetails{})
					Expect(err).To(HaveOccurred())
				})
			})
		})
	})
})
