import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {CreateDeploymentPlanRequest, DeploymentPlan} from '../types/deployment-plan';

const baseUrl = '/api/v1/deployment-plans';

@Injectable({
  providedIn: 'root',
})
export class DeploymentPlansService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<DeploymentPlan[]> {
    return this.httpClient.get<DeploymentPlan[]>(baseUrl);
  }

  get(id: string): Observable<DeploymentPlan> {
    return this.httpClient.get<DeploymentPlan>(`${baseUrl}/${id}`);
  }

  create(request: CreateDeploymentPlanRequest): Observable<DeploymentPlan> {
    return this.httpClient.post<DeploymentPlan>(baseUrl, request);
  }
}
