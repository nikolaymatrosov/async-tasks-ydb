status:
  port: 16241
  host: null

agent_log:
  priority: NOTICE

storages:
  - name: main
    plugin: fs
    config:
      directory: /var/lib/yandex/unified_agent/main
      max_partition_size: 100mb
      max_segment_size: 10mb

channels:
  - name: cloud_monitoring
    channel:
      pipe:
        - storage_ref:
            name: main
      output:
        plugin: yc_metrics
        config:
          url: https://monitoring.api.cloud.yandex.net/monitoring/v2/data/write
          folder_id: "${folder_id}"
          iam:
            cloud_meta: {}

routes:
  - input:
      plugin: linux_metrics
      config:
        namespace: sys
        proc_directory: /ua_proc
        sys_directory: /sys
        resources:
          cpu: basic
          memory: basic
          network: basic
          storage: basic
          io: basic
          kernel: basic
    channel:
      channel_ref:
        name: cloud_monitoring

  - input:
      plugin: agent_metrics
      config:
        namespace: ua
    channel:
      pipe:
        - filter:
            plugin: filter_metrics
            config:
              match: "{scope=health}"
      channel_ref:
        name: cloud_monitoring

  - input:
      plugin: metrics_pull
      config:
        url: ${metrics_url}
        format:
          prometheus: {}
        poll_period: 15s
    channel:
      channel_ref:
        name: cloud_monitoring
