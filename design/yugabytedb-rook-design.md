YugaByte DB is a high-performance distributed transactional database. (More information [here](https://docs.yugabyte.com/latest/introduction/)). This issue outlines design and interfaces for a Rook YugaByte operator for running YugaByte on Kubernetes with Rook as the underlying storage engine.

**What is use case behind this feature:**

A cluster of YugaByte DB backed by Rook should be created using the `clusters.yugabytedb.rook.io` custom resource definition. Below is a sample custom resource spec for creating a 3-master, 3 t-servers cluster using the CRD. The sample is followed by an explanation of different configuration options available on the YugaByte DB CRD.

## Sample

```yaml
apiVersion: yugabytedb.rook.io/v1alpha1
kind: Cluster
metadata:
  name: rook-yugabytedb
  namespace: rook-yugabytedb
spec:
  # Mentioning network ports is optional. If some or all ports are not specified, then they will be defaulted to below-mentioned values, except for tserver-ui.
  # For tserver-ui a cluster ip service will be created if the yb-tserver-ui port is explicitly mentioned. If it is not specified, only StatefulSet & headless service will be created for TServer. TServer ClusterIP service creation will be skipped. Whereas for Master, all 3 kubernetes objects will always be created.
  network:
    ports:
      - name: yb-master-ui
        port: 7000          # default value
      - name: yb-master-grpc
        port: 7100          # default value
      - name: yb-tserver-ui
        port: 9000          # default value
      - name: yb-tserver-grpc
        port: 9100          # default value
      - name: yb-tserver-cassandra
        port: 9042          # default value
      - name: yb-tserver-redis
        port: 6379          # default value
      - name: yb-tserver-postgres
        port: 5433          # default value
  # Replica count for master & tserver
  replicas:
    master: 3
    tserver: 3
  scope:
    ## Volume claim templates for Master & TServer pods
    master:
      volumeClaimTemplates:
      - metadata:
          name: datadir
        spec:
          accessModes: [ "ReadWriteOnce" ]
          resources:
            requests:
              storage: 1Gi
          storageClassName: standard
    tserver:
      volumeClaimTemplates:
      - metadata:
          name: datadir
        spec:
          accessModes: [ "ReadWriteOnce" ]
          resources:
            requests:
              storage: 1Gi
          storageClassName: standard
```
# Cluster Settings

### Network
`network` field accepts `NetworkSpec` to be specified which describes YugabyteDB network settings. This is an **optional** field. Default network settings will be used, if any or all of the acceptable values are absent.

A ClusterIP service will be created when `yb-tserver-ui` port is explicitly specified. If it is not specified, only StatefulSet & headless service will be created for TServer. ClusterIP service creation will be skipped. Whereas for Master, all 3 kubernetes objects will always be created.

The acceptable port names & their default values are as follows:

| Name | Default Value |
| ---- | ------------- |
| yb-master-ui | 7000 |
| yb-master-grpc | 7100 |
| yb-tserver-ui | 9000 |
| yb-tserver-grpc | 9100 |
| yb-tserver-cassandra | 9042 |
| yb-tserver-redis | 6379 |
| yb-tserver-postgres | 5433 |


### Replica Count
Specify replica count for `master` & `tserver` pods under `replicas` field. This is a **required** field.

### Storage Scope
Specify `StorageScopeSpec` under the `scope` field for `master` & `tserver` each. This is a **required** field. Each `StorageScopeSpec` is a list of PersistentVolumeClaim templates, containing **exactly one** PersistentVolumeClaim template.
