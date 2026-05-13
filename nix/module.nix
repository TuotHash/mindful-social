# NixOS module for the Mindful Social server.
#
# Exposes the binary as a systemd service on 127.0.0.1, expects a
# reverse proxy (HAProxy / Caddy / nginx) to handle TLS upstream. By
# default provisions a local PostgreSQL database and a peer-authenticated
# unix socket; set services.mindful-social.database.createLocally to
# false to point at an external Postgres via DATABASE_URL.
{ config, lib, pkgs, ... }:

let
  cfg = config.services.mindful-social;
in {
  options.services.mindful-social = {
    enable = lib.mkEnableOption "the Mindful Social server";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix {};
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix {}";
      description = ''
        Mindful Social package to run. Defaults to building from the
        sibling package.nix, so the module works standalone on channels
        without the flake.
      '';
    };

    listenAddr = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      example = "127.0.0.1:8080";
      description = ''
        TCP address the HTTP server binds to. Keep it on 127.0.0.1 and
        terminate TLS in a reverse proxy in front of it.
      '';
    };

    publicBaseURL = lib.mkOption {
      type = lib.types.str;
      example = "https://mindful.example.org";
      description = ''
        Absolute origin the browser sees. OAuth callback URLs derive
        from this, so it must match the URL users actually visit.
      '';
    };

    signupEnabled = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Whether the email + password signup form is open. Setting this
        to false closes signups; OAuth/SSO logins still create accounts
        for new users.
      '';
    };

    adminUsers = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      example = [ "alice" "bob" ];
      description = ''
        Usernames promoted to the admin role on every startup. Unknown
        usernames are logged and skipped. Demotions through the admin
        UI stick — this list only grants admin, never revokes.
      '';
    };

    environment = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = {};
      example = {
        GOOGLE_CLIENT_ID = "1234.apps.googleusercontent.com";
      };
      description = ''
        Additional environment variables merged into the systemd unit.
        Useful for non-secret OAuth client IDs and OIDC discovery URLs.
        Put actual secrets in environmentFile instead so they stay out
        of the Nix store.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/mindful-social.env";
      description = ''
        Path to a systemd EnvironmentFile holding secret variables
        (OAuth client secrets, DATABASE_URL for external databases).
        Typically managed by sops-nix or agenix so the file is
        decrypted into /run/secrets at boot and never written to the
        Nix store.
      '';
    };

    database = {
      createLocally = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          When true, enables PostgreSQL on the host, creates the
          mindful_social database and user, and points the service at
          the local unix socket using peer authentication. When false,
          DATABASE_URL must be supplied via environmentFile.
        '';
      };

      name = lib.mkOption {
        type = lib.types.str;
        default = "mindful-social";
        description = "Local database name. Only used when createLocally is true.";
      };

      user = lib.mkOption {
        type = lib.types.str;
        default = "mindful-social";
        description = "Local database role and matching system user. Only used when createLocally is true.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    # Local Postgres provisioning. ensureUsers grants the system user
    # access to its own database via peer auth on the unix socket — no
    # password required, no DATABASE_URL secret needed.
    services.postgresql = lib.mkIf cfg.database.createLocally {
      enable = true;
      ensureDatabases = [ cfg.database.name ];
      ensureUsers = [{
        name = cfg.database.user;
        ensureDBOwnership = true;
      }];
    };

    # A static system user is required when talking to local Postgres
    # via peer auth: the OS user name has to match the database role.
    # DynamicUser would generate a fresh UID every boot and break that.
    users.users = lib.mkIf cfg.database.createLocally {
      "${cfg.database.user}" = {
        isSystemUser = true;
        group = cfg.database.user;
        description = "Mindful Social service user";
      };
    };

    users.groups = lib.mkIf cfg.database.createLocally {
      "${cfg.database.user}" = {};
    };

    systemd.services.mindful-social = {
      description = "Mindful Social — community discourse server";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ]
        ++ lib.optional cfg.database.createLocally "postgresql.service";
      wants = [ "network-online.target" ]
        ++ lib.optional cfg.database.createLocally "postgresql.service";

      environment = {
        LISTEN_ADDR = cfg.listenAddr;
        PUBLIC_BASE_URL = cfg.publicBaseURL;
        SIGNUP_ENABLED = if cfg.signupEnabled then "true" else "false";
        # Matches StateDirectory below. systemd creates and owns this
        # path, so it's the one place the sandboxed unit can write.
        DATA_DIR = "/var/lib/mindful-social";
      }
      // lib.optionalAttrs (cfg.adminUsers != []) {
        ADMIN_USERS = lib.concatStringsSep "," cfg.adminUsers;
      }
      // lib.optionalAttrs cfg.database.createLocally {
        DATABASE_URL = "postgres:///${cfg.database.name}?host=/run/postgresql";
      }
      // cfg.environment;

      serviceConfig = {
        ExecStart = lib.getExe cfg.package;
        Restart = "on-failure";
        RestartSec = 5;

        User = if cfg.database.createLocally then cfg.database.user else "mindful-social";
        Group = if cfg.database.createLocally then cfg.database.user else "mindful-social";
        DynamicUser = !cfg.database.createLocally;

        StateDirectory = "mindful-social";
        StateDirectoryMode = "0750";

        EnvironmentFile = lib.mkIf (cfg.environmentFile != null) cfg.environmentFile;

        # Sandboxing. The service only needs to bind a localhost TCP
        # port, talk to Postgres (unix socket or TCP), and reach OAuth
        # IdPs over HTTPS. Everything else gets locked down.
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        ProtectHostname = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        NoNewPrivileges = true;
        RestrictSUIDSGID = true;
        RestrictRealtime = true;
        RestrictNamespaces = true;
        LockPersonality = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [ "@system-service" "~@privileged" "~@resources" ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        CapabilityBoundingSet = [];
        AmbientCapabilities = [];
        UMask = "0077";
      };
    };
  };
}
