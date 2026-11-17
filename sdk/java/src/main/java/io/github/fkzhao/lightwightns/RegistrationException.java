package io.github.fkzhao.lightwightns;

public final class RegistrationException extends RuntimeException {
    private final int status;
    private final String code;

    public RegistrationException(int status, String code) {
        super("Registration API " + status + ": " + code);
        this.status = status;
        this.code = code;
    }

    public int status() { return status; }
    public String code() { return code; }
}
