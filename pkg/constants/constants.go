package constants

const IsAnyCommonParameterChanged = "isAnyCommonParameterChanged"

const ContextSpec = "contextSpec"
const ContextSpecHasChanges = "contextSpecHasChanges"
const ContextSchema = "contextSchema"
const ContextRequest = "contextRequest"
const ContextClient = "contextClient"
const ContextKubeClient = "contextKubeClient"
const ContextLogger = "contextLogger"
const ContextVault = "contextVault"
const ContextConsul = "contextConsul"
const ContextConsulServiceRegistrations = "contextConsulServiceRegistrations"
const ContextConsulRegistration = "contextConsulRegistration"

const ContextServiceDeployType = "contextServiceDeployType"

const LastApplliedName = "last-applied-configuration-info"

const ContextServiceDeploymentInfo = "serviceDeploymentInfo"
const MicroServiceSuccessDeploymentResult = "success"

//Struct constants
const KubernetesHelperImpl = "kubernetesHelperImpl"
const ServicesUsersContextList = "ctxServicesUsersList"

//common constants
const BashCommand = "bash"
const TriesCount = "triesCount"
const RetryTimeoutSec = "retryTimeout"

//kubernetes constants
const StatefulSetPodNameTemplate = "%s-0"
const ClusterDomainTemplate = "%s.svc.cluster.local"
const ServiceClusterDomainTemplate = "%s." + ClusterDomainTemplate
const ReplicaNumber = "replica_number"
const RecyclerNameTemplate = "pv-recycler-pvc-%s"
const RecyclerPod = "recycler-pod"
const Microservice = "microservice"
const KubeHostName = "kubernetes.io/hostname"
const ServiceName = "serviceName"
const Name = "name"
const Service = "service"
const App = "app"
const Data = "data"
const Tmp = "tmp"
const Username = "username"
const Password = "password"

//vault
const TokenFilePath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
