"use strict";

angular.module("openshiftConsole")

  .service("ApplicationGenerator", function(DataService, Logger, $parse){

    var scope = {};

    scope._generateSecret = function(){
        //http://stackoverflow.com/questions/105034/create-guid-uuid-in-javascript
        function s4() {
          return Math.floor((1 + Math.random()) * 0x10000)
              .toString(16)
              .substring(1);
          }
        return s4()+s4()+s4()+s4();
      };

    scope.parsePorts = function(image) {
      //map ports to k8s structure
      var parsePortsFromSpec = function(portSpec){
        var ports = [];
        angular.forEach(portSpec, function(value, key){
          var parts = key.split("/");
          if(parts.length === 1){
            parts.push("tcp");
          }

          var containerPort = parseInt(parts[0], 10);
          if (isNaN(containerPort)) {
            Logger.warn("Container port " + parts[0] + " is not a number for image " + $parse("metadata.name")(image));
          } else {
            ports.push({
              containerPort: containerPort,
              protocol: parts[1].toUpperCase()
            });
          }
        });

        // Since the exposed ports in Docker image metadata are not in any
        // order, sort the ports from lowest to highest.
        ports.sort(function(left, right) {
          return left.containerPort - right.containerPort;
        });

        return ports;
      };

      var specPorts =
        $parse('dockerImageMetadata.Config.ExposedPorts')(image) ||
        $parse('dockerImageMetadata.ContainerConfig.ExposedPorts')(image) ||
        [];
      return parsePortsFromSpec(specPorts);
    };

    /**
     * Generate resource definitions to support the given input
     * @param {type} input
     * @returns Hash of resource definitions
     */
    scope.generate = function(input){
      var ports = scope.parsePorts(input.image);

      //augment labels
      input.labels.app = input.name;
      input.annotations["openshift.io/generated-by"] = "OpenShiftWebConsole";

      var imageSpec;
      if(input.buildConfig.sourceUrl !== null){
        imageSpec = {
          name: input.name,
          tag: "latest",
          kind: "ImageStreamTag",
          toString: function(){
            return this.name + ":" + this.tag;
          }
        };
      }

      var resources = {
        imageStream: scope._generateImageStream(input),
        buildConfig: scope._generateBuildConfig(input, imageSpec, input.labels),
        deploymentConfig: scope._generateDeploymentConfig(input, imageSpec, ports, input.labels)
      };

      var service = scope._generateService(input, input.name, ports);
      if (service) {
        resources.service = service;
        // Only attempt to generate a route if there is a service.
        resources.route = scope._generateRoute(input, input.name, resources.service.metadata.name);
      }

      return resources;
    };

    // Public method for creating a route.
    // Expects routeOptions from the osc-routing directive.
    scope.createRoute = function(routeOptions, serviceName, labels) {
      return scope._generateRoute({
        labels: labels || {},
        routing: angular.extend({ include: true }, routeOptions)
      }, routeOptions.name, serviceName);
    };

    scope._generateRoute = function(input, name, serviceName){
      if(!input.routing.include) {
        return null;
      }

      var route = {
        kind: "Route",
        apiVersion: "v1",
        metadata: {
          name: name,
          labels: input.labels,
          annotations: input.annotations
        },
        spec: {
          to: {
            kind: "Service",
            name: serviceName
          }
        }
      };

      if (input.routing.host) {
        route.spec.host = input.routing.host;
      }

      if (input.routing.path) {
        route.spec.path = input.routing.path;
      }

      if (input.routing.targetPort) {
        route.spec.port = {
          targetPort: input.routing.targetPort.containerPort
        };
      }

      var tls = input.routing.tls;
      if (tls && tls.termination) {
        route.spec.tls = {
          termination: tls.termination
        };

        if (tls.termination !== 'passthrough') {
          if (tls.termination === 'edge' && tls.insecureEdgeTerminationPolicy) {
            route.spec.tls.insecureEdgeTerminationPolicy = tls.insecureEdgeTerminationPolicy;
          }
          if (tls.certificate) {
            route.spec.tls.certificate = tls.certificate;
          }
          if (tls.key) {
            route.spec.tls.key = tls.key;
          }
          if (tls.caCertificate) {
            route.spec.tls.caCertificate = tls.caCertificate;
          }
          if (tls.destinationCACertificate && tls.termination === 'reencrypt') {
            route.spec.tls.destinationCACertificate = tls.destinationCACertificate;
          }
        }
      }

      return route;
    };

    scope._generateDeploymentConfig = function(input, imageSpec, ports){
      var env = [];
      angular.forEach(input.deploymentConfig.envVars, function(value, key){
        env.push({name: key, value: value});
      });
      var templateLabels = angular.copy(input.labels);
      templateLabels.deploymentconfig = input.name;

      var container = {
        image: imageSpec.toString(),
        name: input.name,
        ports: ports,
        env: env,
        resources: _.get(input, "container.resources")
      };

      var deploymentConfig = {
        apiVersion: "v1",
        kind: "DeploymentConfig",
        metadata: {
          name: input.name,
          labels: input.labels,
          annotations: input.annotations
        },
        spec: {
          replicas: input.scaling.replicas,
          selector: {
            deploymentconfig: input.name
          },
          triggers: [],
          template: {
            metadata: {
              labels: templateLabels
            },
            spec: {
              containers: [ container ]
            }
          }
        }
      };
      if(input.deploymentConfig.deployOnNewImage){
        deploymentConfig.spec.triggers.push(
          {
            type: "ImageChange",
            imageChangeParams: {
              automatic: true,
              containerNames: [
                input.name
              ],
              from: {
                kind: imageSpec.kind,
                name: imageSpec.toString()
              }
            }
          }
        );
      }
      if(input.deploymentConfig.deployOnConfigChange){
        deploymentConfig.spec.triggers.push({type: "ConfigChange"});
      }
      return deploymentConfig;
    };

    scope._generateBuildConfig = function(input, imageSpec){
      var env = [];
      angular.forEach(input.buildConfig.envVars, function(value, key){
        env.push({name: key, value: value});
      });
      var triggers = [
        {
          generic: {
            secret: scope._generateSecret()
          },
          type: "Generic"
        }
      ];
      if (input.buildConfig.buildOnSourceChange) {
        triggers.push({
            github: {
              secret: scope._generateSecret()
            },
            type: "GitHub"
          }
        );
      }
      if (input.buildConfig.buildOnImageChange) {
        triggers.push({
          imageChange: {},
          type: "ImageChange"
        });
      }
      if (input.buildConfig.buildOnConfigChange) {
        triggers.push({
          type: "ConfigChange"
        });
      }

      // User can input a URL that contains a ref
      var uri = new URI(input.buildConfig.sourceUrl);
      var sourceRef = uri.fragment();
      if (!sourceRef) {
        sourceRef = "master";
      }
      uri.fragment("");
      var sourceUrl = uri.href();

      var bc = {
        apiVersion: "v1",
        kind: "BuildConfig",
        metadata: {
          name: input.name,
          labels: input.labels,
          annotations: input.annotations
        },
        spec: {
          output: {
            to: {
              name: imageSpec.toString(),
              kind: imageSpec.kind
            }
          },
          source: {
            git: {
              ref: input.buildConfig.gitRef || sourceRef,
              uri: sourceUrl
            },
            type: "Git"
          },
          strategy: {
            type: "Source",
            sourceStrategy: {
              from: {
                kind: "ImageStreamTag",
                name: input.imageName + ":" + input.imageTag,
                namespace: input.namespace
              },
              env: env
            }
          },
          triggers: triggers
        }
      };

      // Add contextDir only if specified.
      if (input.buildConfig.contextDir) {
        bc.spec.source.contextDir = input.buildConfig.contextDir;
      }

      return bc;
    };

    scope._generateImageStream = function(input){
      return {
        apiVersion: "v1",
        kind: "ImageStream",
        metadata: {
          name: input.name,
          labels: input.labels,
          annotations: input.annotations
        }
      };
    };

    scope._generateService  = function(input, serviceName, ports){
      if (!ports || !ports.length) {
        return null;
      }

      var service = {
        kind: "Service",
        apiVersion: "v1",
        metadata: {
          name: serviceName,
          labels: input.labels,
          annotations: input.annotations
        },
        spec: {
          selector: {
            deploymentconfig: input.name
          },
          ports: []
        }
      };

      angular.forEach(ports, function(port) {
        service.spec.ports.push({
          port: port.containerPort,
          targetPort: port.containerPort,
          protocol: port.protocol,
          // Use the same naming convention as CLI new-app.
          name: (port.containerPort + '-' + port.protocol).toLowerCase()
        });
      });

      return service;
    };

    return scope;
  }
);
