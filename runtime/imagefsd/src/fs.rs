#[cfg(target_os = "linux")]
mod imp {
    use fuse_backend_rs::api::server::Server as FuseServer;
    use fuse_backend_rs::transport::{FuseChannel, FuseSession, Writer};
    use signal::trap::Trap;
    use signal::Signal;
    use std::io;
    use std::path::PathBuf;
    use std::sync::Arc;
    use tracing::{error, info};

    pub struct Worker<T: fuse_backend_rs::api::filesystem::FileSystem + Send + Sync + 'static> {
        server: Arc<FuseServer<T>>,
        ch: FuseChannel,
    }

    impl<T: fuse_backend_rs::api::filesystem::FileSystem + Send + Sync + 'static> Worker<T> {
        pub fn new(session: &FuseSession, server: Arc<FuseServer<T>>) -> anyhow::Result<Self> {
            Ok(Self {
                server,
                ch: session.new_channel()?,
            })
        }

        fn svc_loop(&mut self) -> anyhow::Result<()> {
            let ebadf = io::Error::from_raw_os_error(libc::EBADF);
            while let Some((reader, writer)) = self.ch.get_request()? {
                if let Err(e) =
                    self.server
                        .handle_message(reader, Writer::FuseDev(writer), None, None)
                {
                    match e {
                        fuse_backend_rs::Error::EncodeMessage(err)
                            if err.raw_os_error() == ebadf.raw_os_error() =>
                        {
                            break;
                        }
                        _ => continue,
                    }
                }
            }
            Ok(())
        }

        pub fn run(mut self) -> anyhow::Result<()> {
            self.svc_loop()
        }
    }

    pub fn mount_fs<T>(fs: T, mountpoint: &str, worker_num: u32) -> anyhow::Result<()>
    where
        T: fuse_backend_rs::api::filesystem::FileSystem + Send + Sync + 'static,
    {
        let path_buf = PathBuf::from(mountpoint.to_string());
        let mut session = FuseSession::new(path_buf.as_path(), "imagefsd", "imagefsd", true)?;
        session.mount()?;
        let mut trap = Trap::trap(&[Signal::SIGTERM, Signal::SIGINT]);
        let fuse_server = Arc::new(FuseServer::new(fs));

        for _ in 0..worker_num {
            let worker = Worker::new(&session, fuse_server.clone())?;
            std::thread::spawn(move || {
                if let Err(e) = worker.run() {
                    error!(err = debug(e), "fuse worker exited");
                }
            });
        }
        if trap.next().is_some() {
            info!(mnt = display(mountpoint), "Umount fs.");
        }
        Ok(())
    }
}

#[cfg(not(target_os = "linux"))]
mod imp {
    pub fn mount_fs<T>(_fs: T, _mountpoint: &str, _worker_num: u32) -> anyhow::Result<()> {
        Err(std::io::Error::new(
            std::io::ErrorKind::Unsupported,
            "mount is only supported on Linux",
        )
        .into())
    }
}

pub use imp::mount_fs;
