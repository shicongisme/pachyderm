package s3

import (
    "io/ioutil"
    "os"
    "strings"
    "testing"

    minio "github.com/minio/minio-go"

    tu "github.com/pachyderm/pachyderm/src/server/pkg/testutil"
    "github.com/pachyderm/pachyderm/src/client"
    "github.com/pachyderm/pachyderm/src/client/pkg/require"
    "github.com/pachyderm/pachyderm/src/client/pfs"
)

type workerTestState struct {
	pachClient *client.APIClient
	minioClient *minio.Client
	inputRepo string
	outputRepo string
	inputMasterCommit *pfs.Commit
	inputDevelopCommit *pfs.Commit
	outputCommit *pfs.Commit
}

func workerListBuckets(t *testing.T, s *workerTestState) {
	// create a repo - this should not show up list buckets with the worker
	// driver
    repo := tu.UniqueString("testlistbuckets1")
    require.NoError(t, s.pachClient.CreateRepo(repo))
    require.NoError(t, s.pachClient.CreateBranch(repo, "master", "", nil))

    buckets, err := s.minioClient.ListBuckets()
    require.NoError(t, err)

    actualBucketNames := []string{}
    for _, bucket := range buckets {
    	actualBucketNames = append(actualBucketNames, bucket.Name)
    }

    require.ElementsEqual(t, []string{"in1", "in2", "out"}, actualBucketNames)
}

func workerGetObject(t *testing.T, s *workerTestState) {
    fetchedContent, err := getObject(t, s.minioClient, "in1", "file")
    require.NoError(t, err)
    require.Equal(t, "foo", fetchedContent)
}

func workerGetObjectOutputRepo(t *testing.T, s *workerTestState) {
    _, err := getObject(t, s.minioClient, "out", "file")
    keyNotFoundError(t, err)
}

func workerStatObject(t *testing.T, s *workerTestState) {
    info, err := s.minioClient.StatObject("in1", "file", minio.StatObjectOptions{})
    require.NoError(t, err)
    require.True(t, len(info.ETag) > 0)
    require.Equal(t, "text/plain; charset=utf-8", info.ContentType)
    require.Equal(t, int64(3), info.Size)
}

func workerPutObject(t *testing.T, s *workerTestState) {
    r := strings.NewReader("content1")
    _, err := s.minioClient.PutObject("out", "file", r, int64(r.Len()), minio.PutObjectOptions{ContentType: "text/plain"})
    require.NoError(t, err)

    // this should act as a PFS PutFileOverwrite
    r2 := strings.NewReader("content2")
    _, err = s.minioClient.PutObject("out", "file", r2, int64(r2.Len()), minio.PutObjectOptions{ContentType: "text/plain"})
    require.NoError(t, err)

    _, err = getObject(t, s.minioClient, "out", "file")
    keyNotFoundError(t, err)
}

func workerPutObjectInputRepo(t *testing.T, s *workerTestState) {
    r := strings.NewReader("content1")
    _, err := s.minioClient.PutObject("in1", "file", r, int64(r.Len()), minio.PutObjectOptions{ContentType: "text/plain"})
    notImplementedError(t, err)
}

func workerRemoveObject(t *testing.T, s *workerTestState) {
    _, err := s.pachClient.PutFile(s.outputRepo, s.outputCommit.ID, "file", strings.NewReader("content"))
    require.NoError(t, err)

    // as per PFS semantics, the second delete should be a no-op
    require.NoError(t, s.minioClient.RemoveObject("out", "file"))
    require.NoError(t, s.minioClient.RemoveObject("out", "file"))
}

func workerRemoveObjectInputRepo(t *testing.T, s *workerTestState) {
    err := s.minioClient.RemoveObject("in1", "file")
    notImplementedError(t, err)
}

// Tests inserting and getting files over 64mb in size
func workerLargeObjects(t *testing.T, s *workerTestState) {
    // create a temporary file to put ~65mb of contents into it
    inputFile, err := ioutil.TempFile("", "pachyderm-test-large-objects-input-*")
    require.NoError(t, err)
    defer os.Remove(inputFile.Name())
    n, err := inputFile.WriteString(strings.Repeat("no tv and no beer make homer something something.\n", 1363149))
    require.NoError(t, err)
    require.Equal(t, n, 68157450)
    require.NoError(t, inputFile.Sync())

    // first ensure that putting into a repo that doesn't exist triggers an
    // error
    _, err = s.minioClient.FPutObject("foobar", "file", inputFile.Name(), minio.PutObjectOptions{
        ContentType: "text/plain",
    })
    bucketNotFoundError(t, err)

    // now try putting into a legit repo
    l, err := s.minioClient.FPutObject("out", "file", inputFile.Name(), minio.PutObjectOptions{
        ContentType: "text/plain",
    })
    require.NoError(t, err)
    require.Equal(t, int(l), 68157450)

    // try getting an object that does not exist
    err = s.minioClient.FGetObject("foobar", "file", "foo", minio.GetObjectOptions{})
    bucketNotFoundError(t, err)

    // get the file that does exist, doesn't work because we're reading from
    // an output repo
    outputFile, err := ioutil.TempFile("", "pachyderm-test-large-objects-output-*")
    require.NoError(t, err)
    defer os.Remove(outputFile.Name())
    err = s.minioClient.FGetObject("out", "file", outputFile.Name(), minio.GetObjectOptions{})
    keyNotFoundError(t, err)
}

func workerMakeBucket(t *testing.T, s *workerTestState) {
    repo := tu.UniqueString("testmakebucket")
    notImplementedError(t, s.minioClient.MakeBucket(repo, ""))
}

func workerBucketExists(t *testing.T, s *workerTestState) {
    exists, err := s.minioClient.BucketExists("in1")
    require.NoError(t, err)
    require.True(t, exists)

    exists, err = s.minioClient.BucketExists("out")
    require.NoError(t, err)
    require.True(t, exists)

    exists, err = s.minioClient.BucketExists("foobar")
    require.NoError(t, err)
    require.False(t, exists)
}

// func workerRemoveBucket(t *testing.T, s *workerTestState) {
//     repo := tu.UniqueString("testremovebucket")

//     require.NoError(t, s.pachClient.CreateRepo(repo))
//     require.NoError(t, s.pachClient.CreateBranch(repo, "master", "", nil))
//     require.NoError(t, s.pachClient.CreateBranch(repo, "branch", "", nil))

//     require.NoError(t, s.minioClient.RemoveBucket(fmt.Sprintf("master.%s", repo)))
//     require.NoError(t, s.minioClient.RemoveBucket(fmt.Sprintf("branch.%s", repo)))
// }

// func workerListObjectsPaginated(t *testing.T, s *workerTestState) {
//     // create a bunch of files - enough to require the use of paginated
//     // requests when browsing all files. One file will be included on a
//     // separate branch to ensure it's not returned when querying against the
//     // master branch.
//     // `startTime` and `endTime` will be used to ensure that an object's
//     // `LastModified` date is correct. A few minutes are subtracted/added to
//     // each to tolerate the node time not being the same as the host time.
//     startTime := time.Now().Add(time.Duration(-5) * time.Minute)
//     repo := tu.UniqueString("testlistobjectspaginated")
//     require.NoError(t, s.pachClient.CreateRepo(repo))
//     commit, err := s.pachClient.StartCommit(repo, "master")
//     require.NoError(t, err)
//     for i := 0; i <= 1000; i++ {
//         putListFileTestObject(t, s.pachClient, repo, commit.ID, "", i)
//     }
//     for i := 0; i < 10; i++ {
//         putListFileTestObject(t, s.pachClient, repo, commit.ID, "dir/", i)
//         require.NoError(t, err)
//     }
//     putListFileTestObject(t, s.pachClient, repo, "branch", "", 1001)
//     require.NoError(t, s.pachClient.FinishCommit(repo, commit.ID))
//     endTime := time.Now().Add(time.Duration(5) * time.Minute)

//     // Request that will list all files in master's root
//     ch := s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "", false, make(chan struct{}))
//     expectedFiles := []string{}
//     for i := 0; i <= 1000; i++ {
//         expectedFiles = append(expectedFiles, fmt.Sprintf("%d", i))
//     }
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{"dir/"})

//     // Request that will list all files in master starting with 1
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "1", false, make(chan struct{}))
//     expectedFiles = []string{}
//     for i := 0; i <= 1000; i++ {
//         file := fmt.Sprintf("%d", i)
//         if strings.HasPrefix(file, "1") {
//             expectedFiles = append(expectedFiles, file)
//         }
//     }
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})

//     // Request that will list all files in a directory in master
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "dir/", false, make(chan struct{}))
//     expectedFiles = []string{}
//     for i := 0; i < 10; i++ {
//         expectedFiles = append(expectedFiles, fmt.Sprintf("dir/%d", i))
//     }
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
// }

// func workerListObjectsRecursive(t *testing.T, s *workerTestState) {
//     // `startTime` and `endTime` will be used to ensure that an object's
//     // `LastModified` date is correct. A few minutes are subtracted/added to
//     // each to tolerate the node time not being the same as the host time.
//     startTime := time.Now().Add(time.Duration(-5) * time.Minute)
//     repo := tu.UniqueString("testlistobjectsrecursive")
//     require.NoError(t, s.pachClient.CreateRepo(repo))
//     require.NoError(t, s.pachClient.CreateBranch(repo, "branch", "", nil))
//     require.NoError(t, s.pachClient.CreateBranch(repo, "emptybranch", "", nil))
//     commit, err := s.pachClient.StartCommit(repo, "master")
//     require.NoError(t, err)
//     putListFileTestObject(t, s.pachClient, repo, commit.ID, "", 0)
//     putListFileTestObject(t, s.pachClient, repo, commit.ID, "rootdir/", 1)
//     putListFileTestObject(t, s.pachClient, repo, commit.ID, "rootdir/subdir/", 2)
//     putListFileTestObject(t, s.pachClient, repo, "branch", "", 3)
//     require.NoError(t, s.pachClient.FinishCommit(repo, commit.ID))
//     endTime := time.Now().Add(time.Duration(5) * time.Minute)

//     // Request that will list all files in master
//     expectedFiles := []string{"0", "rootdir/1", "rootdir/subdir/2"}
//     ch := s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})

//     // Requests that will list all files in rootdir
//     expectedFiles = []string{"rootdir/1", "rootdir/subdir/2"}
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "r", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir/", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})

//     // Requests that will list all files in subdir
//     expectedFiles = []string{"rootdir/subdir/2"}
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir/s", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir/subdir", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir/subdir/", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
//     ch = s.minioClient.ListObjects(fmt.Sprintf("master.%s", repo), "rootdir/subdir/2", true, make(chan struct{}))
//     checkListObjects(t, ch, startTime, endTime, expectedFiles, []string{})
// }

func TestWorkerDriver(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration tests in short mode")
    }

    pachClient, err := client.NewForTest()
    require.NoError(t, err)

    inputRepo := tu.UniqueString("testworkerdriverinput")
    require.NoError(t, pachClient.CreateRepo(inputRepo))
    outputRepo := tu.UniqueString("testworkerdriveroutput")
    require.NoError(t, pachClient.CreateRepo(outputRepo))

    // create a master branch on the input repo
    inputMasterCommit, err := pachClient.StartCommit(inputRepo, "master")
	require.NoError(t, err)
	_, err = pachClient.PutFile(inputRepo, inputMasterCommit.ID, "file", strings.NewReader("foo"))
	require.NoError(t, err)
	require.NoError(t, pachClient.FinishCommit(inputRepo, inputMasterCommit.ID))

	// create a develop branch on the input repo
    inputDevelopCommit, err := pachClient.StartCommit(inputRepo, "develop")
	require.NoError(t, err)
	_, err = pachClient.PutFile(inputRepo, inputDevelopCommit.ID, "file", strings.NewReader("foo"))
	require.NoError(t, err)
	require.NoError(t, pachClient.FinishCommit(inputRepo, inputDevelopCommit.ID))

	// create the output branch
    outputCommit, err := pachClient.StartCommit(outputRepo, "master")
	require.NoError(t, err)

    driver := NewWorkerDriver(
        []*Bucket{
            &Bucket{
                Repo: inputRepo,
                Commit: inputMasterCommit.ID,
                Name: "in1",
            },
            &Bucket{
                Repo: inputRepo,
                Commit: inputDevelopCommit.ID,
                Name: "in2",
            },
        },
        &Bucket{
            Repo: outputRepo,
            Commit: outputCommit.ID,
            Name: "out",
        },
    )

    testRunner(t, "worker", driver, func(t *testing.T, pachClient *client.APIClient, minioClient *minio.Client) {
    	s := &workerTestState{
			pachClient: pachClient,
			minioClient: minioClient,
			inputRepo: inputRepo,
			outputRepo: outputRepo,
			inputMasterCommit: inputMasterCommit,
			inputDevelopCommit: inputDevelopCommit,
			outputCommit: outputCommit,
    	}

        t.Run("ListBuckets", func(t *testing.T) {
            workerListBuckets(t, s)
        })
        t.Run("GetObject", func(t *testing.T) {
            workerGetObject(t, s)
        })
        t.Run("GetObjectOutputRepo", func(t *testing.T) {
            workerGetObjectOutputRepo(t, s)
        })
        t.Run("StatObject", func(t *testing.T) {
            workerStatObject(t, s)
        })
        t.Run("PutObject", func(t *testing.T) {
            workerPutObject(t, s)
        })
        t.Run("PutObjectInputRepo", func(t *testing.T) {
            workerPutObjectInputRepo(t, s)
        })
        t.Run("RemoveObject", func(t *testing.T) {
            workerRemoveObject(t, s)
        })
        t.Run("RemoveObjectInputRepo", func(t *testing.T) {
            workerRemoveObjectInputRepo(t, s)
        })
        t.Run("LargeObjects", func(t *testing.T) {
            workerLargeObjects(t, s)
        })
        t.Run("MakeBucket", func(t *testing.T) {
            workerMakeBucket(t, s)
        })
        t.Run("BucketExists", func(t *testing.T) {
            workerBucketExists(t, s)
        })
        // t.Run("RemoveBucket", func(t *testing.T) {
        //     workerRemoveBucket(t, s)
        // })
        // t.Run("ListObjectsPaginated", func(t *testing.T) {
        //     workerListObjectsPaginated(t, s)
        // })
        // t.Run("ListObjectsRecursive", func(t *testing.T) {
        //     workerListObjectsRecursive(t, s)
        // })
    })
}
