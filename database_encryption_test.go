package topgun_test

import (
	"bytes"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Database secrets encryption", func() {
	configurePipelineAndTeam := func() {
		By("setting a pipeline that contains secrets")
		fly("set-pipeline", "-n", "-c", "pipelines/secrets.yml", "-p", "pipeline-secrets-test")

		By("creating a team with auth")
		setTeamSession := spawnFlyInteractive(
			bytes.NewBufferString("y\n"),
			"set-team",
			"--team-name", "victoria",
			"--github-auth-user", "victoria",
			"--github-auth-client-id", "victorias_id",
			"--github-auth-client-secret", "victorias_secret",
		)
		<-setTeamSession.Exited
	}

	pgDump := func() *gexec.Session {
		dump := exec.Command("pg_dump", "-U", "atc", "-h", dbInstance.IP, "atc")
		dump.Env = append(os.Environ(), "PGPASSWORD=dummy-password")
		dump.Stdin = bytes.NewBufferString("dummy-password\n")
		session, err := gexec.Start(dump, GinkgoWriter, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())
		<-session.Exited
		Expect(session.ExitCode()).To(Equal(0))
		return session
	}

	getPipeline := func() *gexec.Session {
		session := spawnFly("get-pipeline", "-p", "pipeline-secrets-test")
		<-session.Exited
		Expect(session.ExitCode()).To(Equal(0))
		return session
	}

	Describe("A deployment with encryption enabled immediately", func() {
		BeforeEach(func() {
			Deploy("deployments/single-vm-with-encryption.yml")
		})

		It("encrypts pipeline credentials and team auth config", func() {
			configurePipelineAndTeam()

			By("taking a dump")
			session := pgDump()
			Expect(session).ToNot(gbytes.Say("victorias_secret"))
			Expect(session).ToNot(gbytes.Say("resource_secret"))
			Expect(session).ToNot(gbytes.Say("resource_type_secret"))
			Expect(session).ToNot(gbytes.Say("job_secret"))
		})
	})

	Describe("A deployment with encryption initially not configured", func() {
		BeforeEach(func() {
			Deploy("deployments/single-vm.yml")
		})

		Context("with credentials and team auth in plaintext", func() {
			BeforeEach(func() {
				configurePipelineAndTeam()

				By("taking a dump")
				session := pgDump()
				Expect(string(session.Out.Contents())).To(ContainSubstring("victorias_secret"))
				Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
				Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
				Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))
			})

			Context("when redeployed with encryption enabled", func() {
				BeforeEach(func() {
					Deploy("deployments/single-vm-with-encryption.yml")
				})

				It("encrypts pipeline credentials and team auth config", func() {
					By("taking a dump")
					session := pgDump()
					Expect(session).ToNot(gbytes.Say("victorias_secret"))
					Expect(session).ToNot(gbytes.Say("resource_secret"))
					Expect(session).ToNot(gbytes.Say("resource_type_secret"))
					Expect(session).ToNot(gbytes.Say("job_secret"))

					By("getting the pipeline config")
					session = getPipeline()
					Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
					Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
					Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))
				})

				Context("when the encryption key is rotated", func() {
					BeforeEach(func() {
						Deploy("deployments/single-vm-with-rotated-encryption.yml")
					})

					It("can still get and set pipelines", func() {
						By("taking a dump")
						session := pgDump()
						Expect(session).ToNot(gbytes.Say("victorias_secret"))
						Expect(session).ToNot(gbytes.Say("resource_secret"))
						Expect(session).ToNot(gbytes.Say("resource_type_secret"))
						Expect(session).ToNot(gbytes.Say("job_secret"))

						By("getting the pipeline config")
						session = getPipeline()
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))

						By("setting the pipeline again")
						fly("set-pipeline", "-n", "-c", "pipelines/secrets.yml", "-p", "pipeline-secrets-test")

						By("getting the pipeline config again")
						session = getPipeline()
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))
					})
				})

				Context("when an old key is given but all the data is already using the new key", func() {
					BeforeEach(func() {
						Deploy("deployments/single-vm-with-no-longer-used-old-key.yml")
					})

					It("can still get and set pipelines", func() {
						By("taking a dump")
						session := pgDump()
						Expect(session).ToNot(gbytes.Say("victorias_secret"))
						Expect(session).ToNot(gbytes.Say("resource_secret"))
						Expect(session).ToNot(gbytes.Say("resource_type_secret"))
						Expect(session).ToNot(gbytes.Say("job_secret"))

						By("getting the pipeline config")
						session = getPipeline()
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))

						By("setting the pipeline again")
						fly("set-pipeline", "-n", "-c", "pipelines/secrets.yml", "-p", "pipeline-secrets-test")

						By("getting the pipeline config again")
						session = getPipeline()
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))
					})
				})

				Context("when an old key and new key are both given that do not match the key in use", func() {
					var deploy *gexec.Session

					BeforeEach(func() {
						deploy = StartDeploy("deployments/single-vm-with-bogus-keys.yml")
						<-deploy.Exited
						Expect(deploy.ExitCode()).To(Equal(1))
					})

					AfterEach(func() {
						Deploy("deployments/single-vm-with-encryption.yml")
					})

					It("fails to deploy with a useful message", func() {
						Expect(deploy).To(gbytes.Say("Review logs for failed jobs: atc"))
						Expect(boshLogs).To(gbytes.Say("row encrypted with neither old nor new key"))
					})
				})

				Context("when the encryption key is removed", func() {
					BeforeEach(func() {
						Deploy("deployments/single-vm-with-removed-encryption.yml")
					})

					It("decrypts pipeline credentials and team auth config", func() {
						By("taking a dump")
						session := pgDump()
						Expect(string(session.Out.Contents())).To(ContainSubstring("victorias_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))

						By("getting the pipeline config")
						session = getPipeline()
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("resource_type_secret"))
						Expect(string(session.Out.Contents())).To(ContainSubstring("job_secret"))
					})
				})
			})
		})
	})
})
