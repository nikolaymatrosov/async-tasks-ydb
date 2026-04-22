routes:
  - input:
      plugin: metrics_pull
      config:
        url: ${metrics_url}
        format:
          proto: prometheus
        poll_period: 15s
    channel:
      output:
        plugin: yc_metrics
        config:
          folder_id: "${folder_id}"

  - input:
      plugin: linux_metrics
      config:
        proc_directory: /ua_proc
    channel:
      output:
        plugin: yc_metrics
        config:
          folder_id: "${folder_id}"

status:
  port: 16301

auth:
  iam:
    cloud_meta: {}
